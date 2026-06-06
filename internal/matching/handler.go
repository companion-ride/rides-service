package matching

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"rides/internal/database"
)

// HandlerDB abstracts the subset of database.Service that Handler needs.
// Kept narrow so handler_test.go can stub it without implementing the full
// Service. Mirrors the pattern used by OfferManager (matching.DB).
type HandlerDB interface {
	GetRide(ctx context.Context, rideID string) (*database.Ride, error)
	GetSubRide(ctx context.Context, subRideID string) (*database.SubRide, error)
	GetSubRideByRideAndPassenger(ctx context.Context, rideID, passengerID string) (*database.SubRide, error)
	UpdateRideStatus(ctx context.Context, rideID, status string) error
	UpdateSubRideStatus(ctx context.Context, subRideID, status string) error
	CancelActiveSubRides(ctx context.Context, rideID string) error
	CancelSubRideIfActive(ctx context.Context, subRideID string) (bool, error)
	AreAllSubRidesFinished(ctx context.Context, rideID string) (bool, error)
}

// Handler processes inbound messages from the ride-requests Kafka topic.
type Handler struct {
	offerManager *OfferManager
	db           HandlerDB
	producer     MessageSender
}

// NewHandler creates a Handler with all dependencies.
func NewHandler(
	om *OfferManager,
	db HandlerDB,
	producer MessageSender,
) *Handler {
	return &Handler{
		offerManager: om,
		db:           db,
		producer:     producer,
	}
}

func (h *Handler) sendError(ctx context.Context, userID, rideID, message string) {
	if err := h.producer.Send(ctx, TopicRideNotifications, userID, NotificationMessage{
		UserID:  userID,
		Event:   "ride_error",
		RideID:  rideID,
		Message: message,
	}); err != nil {
		slog.Error("failed to send ride_error", "user_id", userID, "ride_id", rideID, "error", err)
	}
}

// HandleMessage deserializes and routes a raw Kafka message.
func (h *Handler) HandleMessage(ctx context.Context, value []byte) {
	var msg InboundMessage
	if err := json.Unmarshal(value, &msg); err != nil {
		slog.Error("failed to unmarshal kafka message", "error", err, "raw_len", len(value))
		return
	}

	slog.Info("kafka message received",
		"action", msg.Action,
		"request_id", msg.RequestID,
		"ride_id", msg.RideID,
		"user_id", msg.UserID,
		"passenger_id", msg.PassengerID,
	)

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
		h.handleCancel(ctx, msg)
	default:
		slog.Warn("unknown action in kafka message", "action", msg.Action)
	}
}

func (h *Handler) handleRideStart(ctx context.Context, msg InboundMessage) {
	ride, err := h.db.GetRide(ctx, msg.RideID)
	if err != nil {
		slog.Error("failed to get ride", "ride_id", msg.RideID, "error", err)
		return
	}
	if ride.DriverID != msg.UserID {
		slog.Warn("user does not own ride", "user_id", msg.UserID, "ride_id", msg.RideID, "driver_id", ride.DriverID)
		h.sendError(ctx, msg.UserID, msg.RideID, "you do not own this ride")
		return
	}
	if ride.Status != database.StatusPending {
		slog.Warn("ride is not pending", "ride_id", msg.RideID, "status", ride.Status)
		h.sendError(ctx, msg.UserID, msg.RideID, "ride is not in pending status")
		return
	}

	if err := h.db.UpdateRideStatus(ctx, msg.RideID, database.StatusActive); err != nil {
		slog.Error("failed to update ride status", "ride_id", msg.RideID, "status", database.StatusActive, "error", err)
		return
	}

	if err := h.producer.Send(ctx, TopicRideUpdates, msg.RideID, RideUpdateMessage{
		RideID:    msg.RideID,
		Event:     "ride_update",
		Status:    database.StatusActive,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		slog.Error("failed to send ride update", "ride_id", msg.RideID, "error", err)
	}

	slog.Info("ride started", "ride_id", msg.RideID, "driver_id", msg.UserID)
}

func (h *Handler) handlePassengerPickup(ctx context.Context, msg InboundMessage) {
	ride, err := h.db.GetRide(ctx, msg.RideID)
	if err != nil {
		slog.Error("failed to get ride", "ride_id", msg.RideID, "error", err)
		return
	}
	if ride.DriverID != msg.UserID {
		slog.Warn("user does not own ride", "user_id", msg.UserID, "ride_id", msg.RideID, "driver_id", ride.DriverID)
		h.sendError(ctx, msg.UserID, msg.RideID, "you do not own this ride")
		return
	}

	sr, err := h.db.GetSubRide(ctx, msg.SubRideID)
	if err != nil {
		slog.Error("failed to get sub-ride", "sub_ride_id", msg.SubRideID, "error", err)
		h.sendError(ctx, msg.UserID, msg.RideID, "sub-ride not found")
		return
	}
	if sr.RideID != msg.RideID {
		slog.Warn("sub-ride does not belong to ride", "sub_ride_id", msg.SubRideID, "ride_id", msg.RideID)
		h.sendError(ctx, msg.UserID, msg.RideID, "sub-ride does not belong to this ride")
		return
	}
	if sr.Status != database.StatusPending {
		slog.Warn("sub-ride is not pending", "sub_ride_id", msg.SubRideID, "status", sr.Status)
		h.sendError(ctx, msg.UserID, msg.RideID, "sub-ride is not in pending status")
		return
	}

	if err := h.db.UpdateSubRideStatus(ctx, msg.SubRideID, database.StatusPickedUp); err != nil {
		slog.Error("failed to update sub-ride status", "sub_ride_id", msg.SubRideID, "status", database.StatusPickedUp, "error", err)
		return
	}

	if err := h.producer.Send(ctx, TopicRideUpdates, msg.RideID, RideUpdateMessage{
		RideID:      msg.RideID,
		Event:       "passenger_picked_up",
		SubRideID:   msg.SubRideID,
		PassengerID: sr.PassengerID,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		slog.Error("failed to send ride update", "ride_id", msg.RideID, "sub_ride_id", msg.SubRideID, "error", err)
	}

	slog.Info("passenger picked up", "ride_id", msg.RideID, "sub_ride_id", msg.SubRideID, "passenger_id", sr.PassengerID, "driver_id", msg.UserID)
}

func (h *Handler) handlePassengerDropoff(ctx context.Context, msg InboundMessage) {
	ride, err := h.db.GetRide(ctx, msg.RideID)
	if err != nil {
		slog.Error("failed to get ride", "ride_id", msg.RideID, "error", err)
		return
	}
	if ride.DriverID != msg.UserID {
		slog.Warn("user does not own ride", "user_id", msg.UserID, "ride_id", msg.RideID, "driver_id", ride.DriverID)
		h.sendError(ctx, msg.UserID, msg.RideID, "you do not own this ride")
		return
	}

	sr, err := h.db.GetSubRide(ctx, msg.SubRideID)
	if err != nil {
		slog.Error("failed to get sub-ride", "sub_ride_id", msg.SubRideID, "error", err)
		h.sendError(ctx, msg.UserID, msg.RideID, "sub-ride not found")
		return
	}
	if sr.RideID != msg.RideID {
		slog.Warn("sub-ride does not belong to ride", "sub_ride_id", msg.SubRideID, "ride_id", msg.RideID)
		h.sendError(ctx, msg.UserID, msg.RideID, "sub-ride does not belong to this ride")
		return
	}
	if sr.Status != database.StatusPickedUp {
		slog.Warn("sub-ride is not picked up", "sub_ride_id", msg.SubRideID, "status", sr.Status)
		h.sendError(ctx, msg.UserID, msg.RideID, "sub-ride is not in picked_up status")
		return
	}

	if err := h.db.UpdateSubRideStatus(ctx, msg.SubRideID, database.StatusCompleted); err != nil {
		slog.Error("failed to update sub-ride status", "sub_ride_id", msg.SubRideID, "status", database.StatusCompleted, "error", err)
		return
	}

	if err := h.producer.Send(ctx, TopicRideUpdates, msg.RideID, RideUpdateMessage{
		RideID:      msg.RideID,
		Event:       "passenger_dropped_off",
		SubRideID:   msg.SubRideID,
		PassengerID: sr.PassengerID,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		slog.Error("failed to send ride update", "ride_id", msg.RideID, "sub_ride_id", msg.SubRideID, "error", err)
	}

	slog.Info("passenger dropped off", "ride_id", msg.RideID, "sub_ride_id", msg.SubRideID, "passenger_id", sr.PassengerID, "driver_id", msg.UserID)
}

func (h *Handler) handleRideFinish(ctx context.Context, msg InboundMessage) {
	ride, err := h.db.GetRide(ctx, msg.RideID)
	if err != nil {
		slog.Error("failed to get ride", "ride_id", msg.RideID, "error", err)
		return
	}
	if ride.DriverID != msg.UserID {
		slog.Warn("user does not own ride", "user_id", msg.UserID, "ride_id", msg.RideID, "driver_id", ride.DriverID)
		h.sendError(ctx, msg.UserID, msg.RideID, "you do not own this ride")
		return
	}
	if ride.Status != database.StatusActive {
		slog.Warn("ride is not active", "ride_id", msg.RideID, "status", ride.Status)
		h.sendError(ctx, msg.UserID, msg.RideID, "ride is not in active status")
		return
	}

	allDone, err := h.db.AreAllSubRidesFinished(ctx, msg.RideID)
	if err != nil {
		slog.Error("failed to check sub-rides", "ride_id", msg.RideID, "error", err)
		return
	}
	if !allDone {
		if err := h.producer.Send(ctx, TopicRideNotifications, msg.UserID, NotificationMessage{
			UserID:  msg.UserID,
			Event:   "ride_error",
			RideID:  msg.RideID,
			Message: "all sub-rides must be completed or cancelled before finishing",
		}); err != nil {
			slog.Error("failed to send ride notification", "ride_id", msg.RideID, "user_id", msg.UserID, "error", err)
		}
		return
	}

	if err := h.db.UpdateRideStatus(ctx, msg.RideID, database.StatusCompleted); err != nil {
		slog.Error("failed to update ride status", "ride_id", msg.RideID, "status", database.StatusCompleted, "error", err)
		return
	}

	if err := h.producer.Send(ctx, TopicRideUpdates, msg.RideID, RideUpdateMessage{
		RideID:    msg.RideID,
		Event:     "ride_update",
		Status:    database.StatusCompleted,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		slog.Error("failed to send ride update", "ride_id", msg.RideID, "error", err)
	}

	// Let chat-service start the 24h close timer on the ride chat room.
	if err := h.producer.Send(ctx, TopicRideEvents, msg.RideID, RideEvent{
		Event:  ChatEventRideCompleted,
		RideID: msg.RideID,
	}); err != nil {
		slog.Error("failed to send ride event for chat", "ride_id", msg.RideID, "error", err)
	}

	slog.Info("ride finished", "ride_id", msg.RideID, "driver_id", msg.UserID)
}

func (h *Handler) handleRideCancel(ctx context.Context, msg InboundMessage) {
	ride, err := h.db.GetRide(ctx, msg.RideID)
	if err != nil {
		slog.Error("failed to get ride", "ride_id", msg.RideID, "error", err)
		return
	}

	if ride.DriverID != msg.UserID {
		slog.Warn("user cannot cancel ride", "user_id", msg.UserID, "ride_id", msg.RideID, "driver_id", ride.DriverID)
		if err := h.producer.Send(ctx, TopicRideNotifications, msg.UserID, NotificationMessage{
			UserID:  msg.UserID,
			Event:   "ride_error",
			RideID:  msg.RideID,
			Message: "only the ride driver can cancel",
		}); err != nil {
			slog.Error("failed to send ride notification", "ride_id", msg.RideID, "user_id", msg.UserID, "error", err)
		}
		return
	}

	if ride.Status == database.StatusCompleted || ride.Status == database.StatusCancelled {
		slog.Warn("ride already in terminal state", "ride_id", msg.RideID, "status", ride.Status)
		h.sendError(ctx, msg.UserID, msg.RideID, "ride is already "+ride.Status)
		return
	}

	if err := h.db.CancelActiveSubRides(ctx, msg.RideID); err != nil {
		slog.Error("failed to cancel active sub-rides", "ride_id", msg.RideID, "error", err)
	}

	if err := h.db.UpdateRideStatus(ctx, msg.RideID, database.StatusCancelled); err != nil {
		slog.Error("failed to update ride status", "ride_id", msg.RideID, "status", database.StatusCancelled, "error", err)
		return
	}

	if err := h.producer.Send(ctx, TopicRideUpdates, msg.RideID, RideUpdateMessage{
		RideID:    msg.RideID,
		Event:     "ride_update",
		Status:    database.StatusCancelled,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		slog.Error("failed to send ride update", "ride_id", msg.RideID, "error", err)
	}

	// Let chat-service close the ride chat room immediately.
	if err := h.producer.Send(ctx, TopicRideEvents, msg.RideID, RideEvent{
		Event:  ChatEventRideCancelled,
		RideID: msg.RideID,
	}); err != nil {
		slog.Error("failed to send ride event for chat", "ride_id", msg.RideID, "error", err)
	}

	slog.Info("ride cancelled", "ride_id", msg.RideID, "driver_id", msg.UserID)
}

// handleCancel routes an ActionCancel inbound message to either the existing
// driver-side handleRideCancel (when the caller is the ride's driver) or the
// new handlePassengerCancel (when the caller is a matched passenger). A
// caller who is neither receives "not a participant in this ride".
func (h *Handler) handleCancel(ctx context.Context, msg InboundMessage) {
	// Body added in Task 5. Stub returns immediately so the file compiles.
	_ = ctx
	_ = msg
}
