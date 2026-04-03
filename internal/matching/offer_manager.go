package matching

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"time"

	"rides/internal/config"
	"rides/internal/database"
	"rides/internal/location"
)

const (
	offerTimeout   = 10 * time.Second
	overallTimeout = 3 * time.Minute
)

// MessageSender abstracts Kafka message sending for testability.
type MessageSender interface {
	Send(ctx context.Context, topic, key string, value any) error
}

// LocationProvider abstracts location lookups for testability.
type LocationProvider interface {
	GetCurrentLocation(ctx context.Context, userID string) (*location.Location, error)
}

// PendingRequest tracks a passenger's ride search in memory.
type PendingRequest struct {
	RequestID    string
	PassengerID  string
	PickupLat    float64
	PickupLon    float64
	DropoffLat   float64
	DropoffLon   float64
	Price        *int
	Preferences  []string
	Baggage      string
	Comment      string
	Candidates   []database.MatchResult
	CurrentIdx   int
	CurrentOffer *database.MatchResult // the driver currently being offered
	OfferTimer   *time.Timer
	OverallTimer *time.Timer
}

// OfferManager handles the offer negotiation state machine.
type OfferManager struct {
	mu       sync.Mutex
	pending  map[string]*PendingRequest // key: request_id
	producer MessageSender
	db       database.Service
	location LocationProvider
	params   *config.MatchingParamsLoader
}

// NewOfferManager creates a new OfferManager.
func NewOfferManager(
	producer MessageSender,
	db database.Service,
	loc LocationProvider,
	params *config.MatchingParamsLoader,
) *OfferManager {
	return &OfferManager{
		pending:  make(map[string]*PendingRequest),
		producer: producer,
		db:       db,
		location: loc,
		params:   params,
	}
}

func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371000
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	return R * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// HandleRideRequest processes a new passenger ride request.
func (om *OfferManager) HandleRideRequest(ctx context.Context, msg InboundMessage) {
	if msg.Pickup == nil || msg.Dropoff == nil {
		slog.Warn("ride request missing pickup/dropoff", "request_id", msg.RequestID)
		return
	}

	slog.Info("ride request received", "request_id", msg.RequestID, "passenger_id", msg.PassengerID)

	mp := om.params.Get()

	candidates, err := om.db.FindMatchingRides(ctx, database.MatchParams{
		PickupLon:     msg.Pickup.Longitude,
		PickupLat:     msg.Pickup.Latitude,
		DropoffLon:    msg.Dropoff.Longitude,
		DropoffLat:    msg.Dropoff.Latitude,
		PickupRadius:  mp.PickupRadiusM,
		DropoffRadius: mp.DropoffRadiusM,
		MaxResults:    20,
	})
	if err != nil {
		slog.Error("failed to find matching rides", "request_id", msg.RequestID, "error", err)
		om.sendNoMatch(ctx, msg.RequestID, msg.PassengerID)
		return
	}

	if len(candidates) == 0 {
		slog.Info("no matching rides found", "request_id", msg.RequestID, "passenger_id", msg.PassengerID)
		om.sendNoMatch(ctx, msg.RequestID, msg.PassengerID)
		return
	}

	req := &PendingRequest{
		RequestID:   msg.RequestID,
		PassengerID: msg.PassengerID,
		PickupLat:   msg.Pickup.Latitude,
		PickupLon:   msg.Pickup.Longitude,
		DropoffLat:  msg.Dropoff.Latitude,
		DropoffLon:  msg.Dropoff.Longitude,
		Price:       msg.Price,
		Preferences: msg.Preferences,
		Baggage:     msg.Baggage,
		Comment:     msg.Comment,
		Candidates:  candidates,
	}

	om.mu.Lock()
	om.pending[msg.RequestID] = req
	req.OverallTimer = time.AfterFunc(overallTimeout, func() {
		om.onOverallTimeout(msg.RequestID)
	})
	om.mu.Unlock()

	om.offerToNextDriver(ctx, msg.RequestID)
}

// HandleOfferResponse processes a driver's accept/reject response.
func (om *OfferManager) HandleOfferResponse(ctx context.Context, msg InboundMessage) {
	om.mu.Lock()
	req, ok := om.pending[msg.RequestID]
	if !ok {
		om.mu.Unlock()
		return
	}
	currentOffer := req.CurrentOffer
	if currentOffer == nil {
		om.mu.Unlock()
		return // no active offer (race with timeout)
	}
	if currentOffer.DriverID != msg.UserID {
		om.mu.Unlock()
		slog.Warn("user attempted to respond to offer for another driver", "user_id", msg.UserID, "driver_id", currentOffer.DriverID, "request_id", msg.RequestID)
		return
	}
	if req.OfferTimer != nil {
		req.OfferTimer.Stop()
	}
	req.CurrentOffer = nil // clear to prevent double-advance from timeout
	om.mu.Unlock()

	if msg.Accepted != nil && *msg.Accepted {
		sr := &database.SubRide{
			RideID:      currentOffer.RideID,
			PassengerID: req.PassengerID,
			PickupLat:   req.PickupLat,
			PickupLon:   req.PickupLon,
			DropoffLat:  req.DropoffLat,
			DropoffLon:  req.DropoffLon,
			StartLat:    currentOffer.PickupPointLat,
			StartLon:    currentOffer.PickupPointLon,
			FinishLat:   currentOffer.DropoffPointLat,
			FinishLon:   currentOffer.DropoffPointLon,
			Status:      database.StatusPending,
			Price:       req.Price,
			Preferences: req.Preferences,
			Baggage:     req.Baggage,
			Comment:     req.Comment,
		}

		if err := om.db.CreateSubRideWithCapacityCheck(ctx, sr); err != nil {
			if err == database.ErrRideAtCapacity {
				slog.Warn("ride at capacity, trying next driver", "ride_id", currentOffer.RideID, "request_id", msg.RequestID)
				om.offerToNextDriver(ctx, msg.RequestID)
				return
			}
			slog.Error("failed to create sub-ride", "ride_id", currentOffer.RideID, "request_id", msg.RequestID, "error", err)
			om.offerToNextDriver(ctx, msg.RequestID)
			return
		}

		if err := om.producer.Send(ctx, TopicDriverAssigned, req.PassengerID, DriverAssignedMessage{
			RideID:       currentOffer.RideID,
			PassengerID:  req.PassengerID,
			DriverID:     currentOffer.DriverID,
			SubRideID:    sr.SubRideID,
			PickupPoint:  OutboundCoord{Lat: currentOffer.PickupPointLat, Lon: currentOffer.PickupPointLon},
			DropoffPoint: OutboundCoord{Lat: currentOffer.DropoffPointLat, Lon: currentOffer.DropoffPointLon},
			PickupWalkM:  currentOffer.PickupWalkM,
			DropoffWalkM: currentOffer.DropoffWalkM,
		}); err != nil {
			slog.Error("failed to send driver assigned", "ride_id", currentOffer.RideID, "passenger_id", req.PassengerID, "error", err)
		}

		slog.Info("driver accepted ride", "ride_id", currentOffer.RideID, "driver_id", currentOffer.DriverID, "passenger_id", req.PassengerID, "sub_ride_id", sr.SubRideID, "request_id", msg.RequestID)

		om.mu.Lock()
		om.cleanup(msg.RequestID)
		om.mu.Unlock()
		return
	}

	// Driver rejected — try next
	slog.Info("driver rejected offer", "driver_id", msg.UserID, "request_id", msg.RequestID)
	om.offerToNextDriver(ctx, msg.RequestID)
}

// HandleSearchCancel processes a passenger cancelling their search.
func (om *OfferManager) HandleSearchCancel(ctx context.Context, msg InboundMessage) {
	om.mu.Lock()
	req, ok := om.pending[msg.RequestID]
	if !ok {
		om.mu.Unlock()
		return
	}
	if msg.UserID != req.PassengerID {
		om.mu.Unlock()
		slog.Warn("user attempted to cancel search for another passenger", "user_id", msg.UserID, "passenger_id", req.PassengerID, "request_id", msg.RequestID)
		return
	}
	currentOffer := req.CurrentOffer
	passengerID := req.PassengerID
	om.cleanup(msg.RequestID)
	om.mu.Unlock()

	if currentOffer != nil {
		if err := om.producer.Send(ctx, TopicRideNotifications, currentOffer.DriverID, NotificationMessage{
			UserID:      currentOffer.DriverID,
			Event:       "search_cancelled",
			RequestID:   msg.RequestID,
			PassengerID: passengerID,
		}); err != nil {
			slog.Error("failed to send search cancelled notification", "driver_id", currentOffer.DriverID, "request_id", msg.RequestID, "error", err)
		}
	}

	slog.Info("search cancelled", "request_id", msg.RequestID, "passenger_id", passengerID)
}

// offerToNextDriver finds the next eligible candidate and sends an offer.
func (om *OfferManager) offerToNextDriver(ctx context.Context, requestID string) {
	for {
		om.mu.Lock()
		req, ok := om.pending[requestID]
		if !ok {
			om.mu.Unlock()
			return
		}
		if req.CurrentIdx >= len(req.Candidates) {
			passengerID := req.PassengerID
			om.cleanup(requestID)
			om.mu.Unlock()
			slog.Info("no more candidates available", "request_id", requestID, "passenger_id", passengerID)
			om.sendNoMatch(ctx, requestID, passengerID)
			return
		}
		cand := req.Candidates[req.CurrentIdx]
		req.CurrentIdx++
		om.mu.Unlock()

		// Filter checks (no lock held — may do HTTP/DB calls)
		count, err := om.db.CountActiveSubRides(ctx, cand.RideID)
		if err != nil {
			slog.Error("failed to count active sub-rides", "ride_id", cand.RideID, "error", err)
			continue
		}
		if count >= cand.MaxPassengers {
			slog.Info("skipping candidate: at capacity", "ride_id", cand.RideID, "driver_id", cand.DriverID, "count", count, "max", cand.MaxPassengers, "request_id", requestID)
			continue
		}

		loc, locErr := om.location.GetCurrentLocation(ctx, cand.DriverID)
		if locErr == nil {
			driverFrac, fracErr := om.db.GetDriverFraction(ctx, cand.RideID, loc.Lon, loc.Lat)
			if fracErr == nil && driverFrac >= cand.PickupFrac {
				slog.Info("skipping candidate: driver already passed pickup", "ride_id", cand.RideID, "driver_id", cand.DriverID, "driver_frac", driverFrac, "pickup_frac", cand.PickupFrac, "request_id", requestID)
				continue
			}
		}

		// Send offer under lock
		om.mu.Lock()
		req, ok = om.pending[requestID]
		if !ok {
			om.mu.Unlock()
			return
		}
		req.CurrentOffer = &cand
		req.OfferTimer = time.AfterFunc(offerTimeout, func() {
			om.onOfferTimeout(requestID, cand.DriverID)
		})
		offer := NotificationMessage{
			UserID:         cand.DriverID,
			Event:          "ride_offer",
			RequestID:      requestID,
			PassengerID:    req.PassengerID,
			Preferences:    req.Preferences,
			Baggage:        req.Baggage,
			Comment:        req.Comment,
			Pickup:         &OutboundCoord{Lat: cand.PickupPointLat, Lon: cand.PickupPointLon},
			Dropoff:        &OutboundCoord{Lat: cand.DropoffPointLat, Lon: cand.DropoffPointLon},
			PickupWalkM:    cand.PickupWalkM,
			DropoffWalkM:   cand.DropoffWalkM,
			TimeoutSeconds: int(offerTimeout.Seconds()),
		}
		if req.Price != nil {
			offer.Price = *req.Price
		}
		om.mu.Unlock()

		if err := om.producer.Send(ctx, TopicRideNotifications, cand.DriverID, offer); err != nil {
			slog.Error("failed to send ride offer", "driver_id", cand.DriverID, "request_id", requestID, "error", err)
		}

		slog.Info("offer sent to driver", "driver_id", cand.DriverID, "ride_id", cand.RideID, "request_id", requestID)
		return
	}
}

func (om *OfferManager) onOfferTimeout(requestID, driverID string) {
	om.mu.Lock()
	req, ok := om.pending[requestID]
	if !ok || req.CurrentOffer == nil {
		om.mu.Unlock()
		return
	}
	req.CurrentOffer = nil
	om.mu.Unlock()

	slog.Info("offer expired", "request_id", requestID, "driver_id", driverID)

	ctx := context.Background()
	if err := om.producer.Send(ctx, TopicRideNotifications, driverID, NotificationMessage{
		UserID:    driverID,
		Event:     "offer_expired",
		RequestID: requestID,
	}); err != nil {
		slog.Error("failed to send offer expired notification", "driver_id", driverID, "request_id", requestID, "error", err)
	}
	om.offerToNextDriver(ctx, requestID)
}

func (om *OfferManager) onOverallTimeout(requestID string) {
	om.mu.Lock()
	req, ok := om.pending[requestID]
	if !ok {
		om.mu.Unlock()
		return
	}
	passengerID := req.PassengerID
	om.cleanup(requestID)
	om.mu.Unlock()
	om.sendNoMatch(context.Background(), requestID, passengerID)
}

func (om *OfferManager) sendNoMatch(ctx context.Context, requestID, passengerID string) {
	if err := om.producer.Send(ctx, TopicRideNotifications, passengerID, NotificationMessage{
		UserID:    passengerID,
		Event:     "ride_no_match",
		RequestID: requestID,
		Reason:    "no_drivers_available",
	}); err != nil {
		slog.Error("failed to send no match notification", "passenger_id", passengerID, "request_id", requestID, "error", err)
	}
}

// cleanup removes a pending request and stops its timers.
// Must be called with om.mu held.
func (om *OfferManager) cleanup(requestID string) {
	req, ok := om.pending[requestID]
	if !ok {
		return
	}
	if req.OfferTimer != nil {
		req.OfferTimer.Stop()
	}
	if req.OverallTimer != nil {
		req.OverallTimer.Stop()
	}
	delete(om.pending, requestID)
}
