package matching

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"rides/internal/database"
)

// Handler processes inbound messages from the ride-requests Kafka topic.
type Handler struct {
	offerManager *OfferManager
	db           database.Service
	producer     MessageSender
}

// NewHandler creates a Handler with all dependencies.
func NewHandler(
	om *OfferManager,
	db database.Service,
	producer MessageSender,
) *Handler {
	return &Handler{
		offerManager: om,
		db:           db,
		producer:     producer,
	}
}

// HandleMessage deserializes and routes a raw Kafka message.
func (h *Handler) HandleMessage(ctx context.Context, value []byte) {
	var msg InboundMessage
	if err := json.Unmarshal(value, &msg); err != nil {
		log.Printf("unmarshal kafka message: %v", err)
		return
	}

	switch msg.Action {
	case ActionRequest:
		h.offerManager.HandleRideRequest(ctx, msg)
	case ActionOfferResponse:
		h.offerManager.HandleOfferResponse(ctx, msg)
	case ActionSearchCancel:
		h.offerManager.HandleSearchCancel(ctx, msg)
	case ActionRideStart:
		h.handleRideStart(ctx, msg)
	case ActionPassengerPickup:
		h.handlePassengerPickup(ctx, msg)
	case ActionPassengerDropoff:
		h.handlePassengerDropoff(ctx, msg)
	case ActionRideFinish:
		h.handleRideFinish(ctx, msg)
	case ActionCancel:
		h.handleRideCancel(ctx, msg)
	default:
		log.Printf("unknown action %q in kafka message", msg.Action)
	}
}

func (h *Handler) handleRideStart(ctx context.Context, msg InboundMessage) {
	ride, err := h.db.GetRide(ctx, msg.RideID)
	if err != nil {
		log.Printf("get ride error: %v", err)
		return
	}
	if ride.DriverID != msg.UserID {
		log.Printf("user %s does not own ride %s", msg.UserID, msg.RideID)
		return
	}
	if ride.Status != database.StatusPending {
		log.Printf("ride %s is not pending (status=%s)", msg.RideID, ride.Status)
		return
	}

	if err := h.db.UpdateRideStatus(ctx, msg.RideID, database.StatusActive); err != nil {
		log.Printf("update ride status error: %v", err)
		return
	}

	h.producer.Send(ctx, TopicRideUpdates, msg.RideID, RideUpdateMessage{
		RideID:    msg.RideID,
		Event:     "ride_update",
		Status:    database.StatusActive,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

func (h *Handler) handlePassengerPickup(ctx context.Context, msg InboundMessage) {
	ride, err := h.db.GetRide(ctx, msg.RideID)
	if err != nil {
		log.Printf("get ride error: %v", err)
		return
	}
	if ride.DriverID != msg.UserID {
		log.Printf("user %s does not own ride %s", msg.UserID, msg.RideID)
		return
	}

	sr, err := h.db.GetSubRide(ctx, msg.SubRideID)
	if err != nil {
		log.Printf("get sub-ride error: %v", err)
		return
	}
	if sr.RideID != msg.RideID {
		log.Printf("sub-ride %s does not belong to ride %s", msg.SubRideID, msg.RideID)
		return
	}
	if sr.Status != database.StatusPending {
		log.Printf("sub-ride %s is not pending (status=%s)", msg.SubRideID, sr.Status)
		return
	}

	if err := h.db.UpdateSubRideStatus(ctx, msg.SubRideID, database.StatusPickedUp); err != nil {
		log.Printf("update sub-ride status error: %v", err)
		return
	}

	h.producer.Send(ctx, TopicRideUpdates, msg.RideID, RideUpdateMessage{
		RideID:      msg.RideID,
		Event:       "passenger_picked_up",
		SubRideID:   msg.SubRideID,
		PassengerID: sr.PassengerID,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	})
}

func (h *Handler) handlePassengerDropoff(ctx context.Context, msg InboundMessage) {
	ride, err := h.db.GetRide(ctx, msg.RideID)
	if err != nil {
		log.Printf("get ride error: %v", err)
		return
	}
	if ride.DriverID != msg.UserID {
		log.Printf("user %s does not own ride %s", msg.UserID, msg.RideID)
		return
	}

	sr, err := h.db.GetSubRide(ctx, msg.SubRideID)
	if err != nil {
		log.Printf("get sub-ride error: %v", err)
		return
	}
	if sr.RideID != msg.RideID {
		log.Printf("sub-ride %s does not belong to ride %s", msg.SubRideID, msg.RideID)
		return
	}
	if sr.Status != database.StatusPickedUp {
		log.Printf("sub-ride %s is not picked_up (status=%s)", msg.SubRideID, sr.Status)
		return
	}

	if err := h.db.UpdateSubRideStatus(ctx, msg.SubRideID, database.StatusCompleted); err != nil {
		log.Printf("update sub-ride status error: %v", err)
		return
	}

	h.producer.Send(ctx, TopicRideUpdates, msg.RideID, RideUpdateMessage{
		RideID:      msg.RideID,
		Event:       "passenger_dropped_off",
		SubRideID:   msg.SubRideID,
		PassengerID: sr.PassengerID,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	})
}

func (h *Handler) handleRideFinish(ctx context.Context, msg InboundMessage) {
	ride, err := h.db.GetRide(ctx, msg.RideID)
	if err != nil {
		log.Printf("get ride error: %v", err)
		return
	}
	if ride.DriverID != msg.UserID {
		log.Printf("user %s does not own ride %s", msg.UserID, msg.RideID)
		return
	}
	if ride.Status != database.StatusActive {
		log.Printf("ride %s is not active (status=%s)", msg.RideID, ride.Status)
		return
	}

	allDone, err := h.db.AreAllSubRidesFinished(ctx, msg.RideID)
	if err != nil {
		log.Printf("check sub-rides error: %v", err)
		return
	}
	if !allDone {
		h.producer.Send(ctx, TopicRideNotifications, msg.UserID, NotificationMessage{
			UserID:  msg.UserID,
			Event:   "ride_error",
			RideID:  msg.RideID,
			Message: "all sub-rides must be completed or cancelled before finishing",
		})
		return
	}

	if err := h.db.UpdateRideStatus(ctx, msg.RideID, database.StatusCompleted); err != nil {
		log.Printf("update ride status error: %v", err)
		return
	}

	h.producer.Send(ctx, TopicRideUpdates, msg.RideID, RideUpdateMessage{
		RideID:    msg.RideID,
		Event:     "ride_update",
		Status:    database.StatusCompleted,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

func (h *Handler) handleRideCancel(ctx context.Context, msg InboundMessage) {
	ride, err := h.db.GetRide(ctx, msg.RideID)
	if err != nil {
		log.Printf("get ride error: %v", err)
		return
	}

	if ride.DriverID != msg.UserID {
		log.Printf("user %s cannot cancel ride %s (owner=%s)", msg.UserID, msg.RideID, ride.DriverID)
		h.producer.Send(ctx, TopicRideNotifications, msg.UserID, NotificationMessage{
			UserID:  msg.UserID,
			Event:   "ride_error",
			RideID:  msg.RideID,
			Message: "only the ride driver can cancel",
		})
		return
	}

	if ride.Status == database.StatusCompleted || ride.Status == database.StatusCancelled {
		log.Printf("ride %s already %s", msg.RideID, ride.Status)
		return
	}

	if err := h.db.UpdateRideStatus(ctx, msg.RideID, database.StatusCancelled); err != nil {
		log.Printf("update ride status error: %v", err)
		return
	}

	h.producer.Send(ctx, TopicRideUpdates, msg.RideID, RideUpdateMessage{
		RideID:    msg.RideID,
		Event:     "ride_update",
		Status:    database.StatusCancelled,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}
