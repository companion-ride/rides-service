package matching

import (
	"context"
	"encoding/json"
	"log/slog"
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
		slog.Error("failed to unmarshal kafka message", "error", err)
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
		return
	}
	if ride.Status != database.StatusPending {
		slog.Warn("ride is not pending", "ride_id", msg.RideID, "status", ride.Status)
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
		return
	}

	sr, err := h.db.GetSubRide(ctx, msg.SubRideID)
	if err != nil {
		slog.Error("failed to get sub-ride", "sub_ride_id", msg.SubRideID, "error", err)
		return
	}
	if sr.RideID != msg.RideID {
		slog.Warn("sub-ride does not belong to ride", "sub_ride_id", msg.SubRideID, "ride_id", msg.RideID)
		return
	}
	if sr.Status != database.StatusPending {
		slog.Warn("sub-ride is not pending", "sub_ride_id", msg.SubRideID, "status", sr.Status)
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
		return
	}

	sr, err := h.db.GetSubRide(ctx, msg.SubRideID)
	if err != nil {
		slog.Error("failed to get sub-ride", "sub_ride_id", msg.SubRideID, "error", err)
		return
	}
	if sr.RideID != msg.RideID {
		slog.Warn("sub-ride does not belong to ride", "sub_ride_id", msg.SubRideID, "ride_id", msg.RideID)
		return
	}
	if sr.Status != database.StatusPickedUp {
		slog.Warn("sub-ride is not picked up", "sub_ride_id", msg.SubRideID, "status", sr.Status)
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
		return
	}
	if ride.Status != database.StatusActive {
		slog.Warn("ride is not active", "ride_id", msg.RideID, "status", ride.Status)
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

	slog.Info("ride cancelled", "ride_id", msg.RideID, "driver_id", msg.UserID)
}
