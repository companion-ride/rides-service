package matching

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"rides/internal/database"
)

// handlerMockDB satisfies HandlerDB. Each method has an optional Func
// hook so individual tests can stub behavior; unset hooks return a
// "not stubbed" error to surface accidental dependencies.
type handlerMockDB struct {
	GetRideFunc                      func(ctx context.Context, rideID string) (*database.Ride, error)
	GetSubRideFunc                   func(ctx context.Context, subRideID string) (*database.SubRide, error)
	GetSubRideByRideAndPassengerFunc func(ctx context.Context, rideID, passengerID string) (*database.SubRide, error)
	UpdateRideStatusFunc             func(ctx context.Context, rideID, status string) error
	UpdateSubRideStatusFunc          func(ctx context.Context, subRideID, status string) error
	CancelActiveSubRidesFunc         func(ctx context.Context, rideID string) error
	CancelSubRideIfActiveFunc        func(ctx context.Context, subRideID string) (bool, error)
	AreAllSubRidesFinishedFunc       func(ctx context.Context, rideID string) (bool, error)
}

func (m *handlerMockDB) GetRide(ctx context.Context, rideID string) (*database.Ride, error) {
	if m.GetRideFunc != nil {
		return m.GetRideFunc(ctx, rideID)
	}
	return nil, errors.New("handlerMockDB: GetRide not stubbed")
}
func (m *handlerMockDB) GetSubRide(ctx context.Context, subRideID string) (*database.SubRide, error) {
	if m.GetSubRideFunc != nil {
		return m.GetSubRideFunc(ctx, subRideID)
	}
	return nil, errors.New("handlerMockDB: GetSubRide not stubbed")
}
func (m *handlerMockDB) GetSubRideByRideAndPassenger(ctx context.Context, rideID, passengerID string) (*database.SubRide, error) {
	if m.GetSubRideByRideAndPassengerFunc != nil {
		return m.GetSubRideByRideAndPassengerFunc(ctx, rideID, passengerID)
	}
	return nil, errors.New("handlerMockDB: GetSubRideByRideAndPassenger not stubbed")
}
func (m *handlerMockDB) UpdateRideStatus(ctx context.Context, rideID, status string) error {
	if m.UpdateRideStatusFunc != nil {
		return m.UpdateRideStatusFunc(ctx, rideID, status)
	}
	return errors.New("handlerMockDB: UpdateRideStatus not stubbed")
}
func (m *handlerMockDB) UpdateSubRideStatus(ctx context.Context, subRideID, status string) error {
	if m.UpdateSubRideStatusFunc != nil {
		return m.UpdateSubRideStatusFunc(ctx, subRideID, status)
	}
	return errors.New("handlerMockDB: UpdateSubRideStatus not stubbed")
}
func (m *handlerMockDB) CancelActiveSubRides(ctx context.Context, rideID string) error {
	if m.CancelActiveSubRidesFunc != nil {
		return m.CancelActiveSubRidesFunc(ctx, rideID)
	}
	return errors.New("handlerMockDB: CancelActiveSubRides not stubbed")
}
func (m *handlerMockDB) CancelSubRideIfActive(ctx context.Context, subRideID string) (bool, error) {
	if m.CancelSubRideIfActiveFunc != nil {
		return m.CancelSubRideIfActiveFunc(ctx, subRideID)
	}
	return false, errors.New("handlerMockDB: CancelSubRideIfActive not stubbed")
}
func (m *handlerMockDB) AreAllSubRidesFinished(ctx context.Context, rideID string) (bool, error) {
	if m.AreAllSubRidesFinishedFunc != nil {
		return m.AreAllSubRidesFinishedFunc(ctx, rideID)
	}
	return false, errors.New("handlerMockDB: AreAllSubRidesFinished not stubbed")
}

// Compile-time check that the mock satisfies the interface.
var _ HandlerDB = (*handlerMockDB)(nil)

// firstNotificationByEvent returns the first NotificationMessage sent on
// TopicRideNotifications whose Event field matches the given name, or nil.
func firstNotificationByEvent(sender *mockSender, event string) *NotificationMessage {
	for _, m := range sender.messages() {
		if m.topic != TopicRideNotifications {
			continue
		}
		n, ok := m.value.(NotificationMessage)
		if !ok {
			continue
		}
		if n.Event == event {
			return &n
		}
	}
	return nil
}

// firstSubRideCancelledFor returns the first SubRideCancelledMessage on
// TopicRideNotifications whose UserID matches, or nil.
func firstSubRideCancelledFor(sender *mockSender, userID string) *SubRideCancelledMessage {
	for _, m := range sender.messages() {
		if m.topic != TopicRideNotifications {
			continue
		}
		c, ok := m.value.(SubRideCancelledMessage)
		if !ok {
			continue
		}
		if c.UserID == userID {
			return &c
		}
	}
	return nil
}

func TestHandleCancel_DriverPath_RoutesToRideCancel(t *testing.T) {
	const rideID = "ride-1"
	const driverID = "driver-1"

	var updatedStatus string
	db := &handlerMockDB{
		GetRideFunc: func(ctx context.Context, id string) (*database.Ride, error) {
			if id != rideID {
				t.Fatalf("GetRide called with %s, want %s", id, rideID)
			}
			return &database.Ride{
				RideID:   rideID,
				DriverID: driverID,
				Status:   database.StatusPending,
			}, nil
		},
		CancelActiveSubRidesFunc: func(ctx context.Context, id string) error { return nil },
		UpdateRideStatusFunc: func(ctx context.Context, id, status string) error {
			updatedStatus = status
			return nil
		},
	}
	sender := &mockSender{}
	h := &Handler{db: db, producer: sender}

	h.handleCancel(context.Background(), InboundMessage{
		Action: ActionCancel,
		RideID: rideID,
		UserID: driverID,
	})

	if updatedStatus != database.StatusCancelled {
		t.Errorf("expected ride to be flipped to cancelled, got UpdateRideStatus=%q", updatedStatus)
	}
	// The dispatcher must NOT call the passenger lookup for a driver.
	if db.GetSubRideByRideAndPassengerFunc == nil {
		// stub-not-set means we never opted into a call hook; ensure no
		// "not stubbed" error path was triggered by checking no ride_error.
		if firstNotificationByEvent(sender, "ride_error") != nil {
			t.Errorf("dispatcher should not have entered passenger branch for driver")
		}
	}
}

// Ensure sql and json are referenced so they remain importable for later tasks.
var _ = sql.ErrNoRows
var _ = json.Marshal
