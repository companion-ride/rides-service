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

func TestHandleCancel_Passenger_HappyPath_Pending(t *testing.T) {
	const rideID = "ride-1"
	const driverID = "driver-1"
	const passengerID = "pass-1"
	const subRideID = "sub-1"

	db := &handlerMockDB{
		GetRideFunc: func(ctx context.Context, id string) (*database.Ride, error) {
			return &database.Ride{RideID: rideID, DriverID: driverID, Status: database.StatusPending}, nil
		},
		GetSubRideByRideAndPassengerFunc: func(ctx context.Context, rID, pID string) (*database.SubRide, error) {
			return &database.SubRide{SubRideID: subRideID, RideID: rideID, PassengerID: passengerID, Status: database.StatusPending}, nil
		},
		CancelSubRideIfActiveFunc: func(ctx context.Context, id string) (bool, error) {
			if id != subRideID {
				t.Fatalf("CancelSubRideIfActive called with %s, want %s", id, subRideID)
			}
			return true, nil
		},
	}
	sender := &mockSender{}
	h := &Handler{db: db, producer: sender}

	h.handleCancel(context.Background(), InboundMessage{
		Action: ActionCancel, RideID: rideID, UserID: passengerID, Reason: "changed mind",
	})

	driverEvt := firstSubRideCancelledFor(sender, driverID)
	if driverEvt == nil {
		t.Fatal("expected sub_ride_cancelled to driver, none sent")
	}
	if driverEvt.SubRideID != subRideID || driverEvt.RideID != rideID || driverEvt.PassengerID != passengerID || driverEvt.Reason != "changed mind" {
		t.Errorf("driver event fields wrong: %+v", driverEvt)
	}

	passEvt := firstSubRideCancelledFor(sender, passengerID)
	if passEvt == nil {
		t.Fatal("expected sub_ride_cancelled to passenger, none sent")
	}
	if passEvt.SubRideID != subRideID || passEvt.RideID != rideID {
		t.Errorf("passenger event fields wrong: %+v", passEvt)
	}

	// Parent ride must not have been touched.
	if firstNotificationByEvent(sender, "ride_update") != nil {
		t.Errorf("ride_update should NOT be emitted on passenger cancel")
	}
}

func TestHandleCancel_Passenger_HappyPath_PickedUp(t *testing.T) {
	const rideID = "ride-1"
	const driverID = "driver-1"
	const passengerID = "pass-1"
	const subRideID = "sub-1"

	db := &handlerMockDB{
		GetRideFunc: func(ctx context.Context, id string) (*database.Ride, error) {
			return &database.Ride{RideID: rideID, DriverID: driverID, Status: database.StatusActive}, nil
		},
		GetSubRideByRideAndPassengerFunc: func(ctx context.Context, rID, pID string) (*database.SubRide, error) {
			return &database.SubRide{SubRideID: subRideID, RideID: rideID, PassengerID: passengerID, Status: database.StatusPickedUp}, nil
		},
		CancelSubRideIfActiveFunc: func(ctx context.Context, id string) (bool, error) {
			return true, nil
		},
	}
	sender := &mockSender{}
	h := &Handler{db: db, producer: sender}

	h.handleCancel(context.Background(), InboundMessage{
		Action: ActionCancel, RideID: rideID, UserID: passengerID, Reason: "emergency",
	})

	if firstSubRideCancelledFor(sender, driverID) == nil {
		t.Fatal("expected driver-bound sub_ride_cancelled")
	}
	if firstSubRideCancelledFor(sender, passengerID) == nil {
		t.Fatal("expected passenger-bound sub_ride_cancelled")
	}
}

func TestHandleCancel_Passenger_AlreadyCancelled_NoEvents(t *testing.T) {
	const rideID = "ride-1"
	const driverID = "driver-1"
	const passengerID = "pass-1"
	const subRideID = "sub-1"

	db := &handlerMockDB{
		GetRideFunc: func(ctx context.Context, id string) (*database.Ride, error) {
			return &database.Ride{RideID: rideID, DriverID: driverID, Status: database.StatusPending}, nil
		},
		GetSubRideByRideAndPassengerFunc: func(ctx context.Context, rID, pID string) (*database.SubRide, error) {
			return &database.SubRide{SubRideID: subRideID, RideID: rideID, PassengerID: passengerID, Status: database.StatusCancelled}, nil
		},
		CancelSubRideIfActiveFunc: func(ctx context.Context, id string) (bool, error) {
			return false, nil
		},
	}
	sender := &mockSender{}
	h := &Handler{db: db, producer: sender}

	h.handleCancel(context.Background(), InboundMessage{
		Action: ActionCancel, RideID: rideID, UserID: passengerID,
	})

	if firstSubRideCancelledFor(sender, driverID) != nil {
		t.Errorf("driver MUST NOT receive sub_ride_cancelled when nothing changed")
	}
	if firstSubRideCancelledFor(sender, passengerID) != nil {
		t.Errorf("passenger MUST NOT receive sub_ride_cancelled when nothing changed")
	}
	errEvt := firstNotificationByEvent(sender, "ride_error")
	if errEvt == nil {
		t.Fatal("expected ride_error notification, none sent")
	}
	if errEvt.UserID != passengerID {
		t.Errorf("expected error to passenger, got user_id=%s", errEvt.UserID)
	}
	if errEvt.Message != "your sub-ride is already cancelled" {
		t.Errorf("unexpected error message: %s", errEvt.Message)
	}
}

func TestHandleCancel_Passenger_AlreadyCompleted_NoEvents(t *testing.T) {
	const rideID = "ride-1"
	const driverID = "driver-1"
	const passengerID = "pass-1"
	const subRideID = "sub-1"

	db := &handlerMockDB{
		GetRideFunc: func(ctx context.Context, id string) (*database.Ride, error) {
			return &database.Ride{RideID: rideID, DriverID: driverID, Status: database.StatusActive}, nil
		},
		GetSubRideByRideAndPassengerFunc: func(ctx context.Context, rID, pID string) (*database.SubRide, error) {
			return &database.SubRide{SubRideID: subRideID, RideID: rideID, PassengerID: passengerID, Status: database.StatusCompleted}, nil
		},
		CancelSubRideIfActiveFunc: func(ctx context.Context, id string) (bool, error) {
			return false, nil
		},
	}
	sender := &mockSender{}
	h := &Handler{db: db, producer: sender}

	h.handleCancel(context.Background(), InboundMessage{
		Action: ActionCancel, RideID: rideID, UserID: passengerID,
	})

	errEvt := firstNotificationByEvent(sender, "ride_error")
	if errEvt == nil || errEvt.Message != "your sub-ride is already completed" {
		t.Fatalf("expected 'your sub-ride is already completed', got %+v", errEvt)
	}
	if firstSubRideCancelledFor(sender, driverID) != nil {
		t.Errorf("driver MUST NOT receive sub_ride_cancelled when nothing changed")
	}
}

func TestHandleCancel_Passenger_LookupDBError(t *testing.T) {
	const rideID = "ride-1"
	const driverID = "driver-1"
	const passengerID = "pass-1"

	cancelCalled := false
	db := &handlerMockDB{
		GetRideFunc: func(ctx context.Context, id string) (*database.Ride, error) {
			return &database.Ride{RideID: rideID, DriverID: driverID, Status: database.StatusPending}, nil
		},
		GetSubRideByRideAndPassengerFunc: func(ctx context.Context, rID, pID string) (*database.SubRide, error) {
			return nil, errors.New("boom")
		},
		CancelSubRideIfActiveFunc: func(ctx context.Context, id string) (bool, error) {
			cancelCalled = true
			return true, nil
		},
	}
	sender := &mockSender{}
	h := &Handler{db: db, producer: sender}

	h.handleCancel(context.Background(), InboundMessage{
		Action: ActionCancel, RideID: rideID, UserID: passengerID,
	})

	if cancelCalled {
		t.Error("CancelSubRideIfActive must not be called when lookup fails")
	}
	errEvt := firstNotificationByEvent(sender, "ride_error")
	if errEvt == nil || errEvt.Message != "internal error" {
		t.Fatalf("expected 'internal error' ride_error, got %+v", errEvt)
	}
}

func TestHandleCancel_Passenger_CancelDBError(t *testing.T) {
	const rideID = "ride-1"
	const driverID = "driver-1"
	const passengerID = "pass-1"
	const subRideID = "sub-1"

	db := &handlerMockDB{
		GetRideFunc: func(ctx context.Context, id string) (*database.Ride, error) {
			return &database.Ride{RideID: rideID, DriverID: driverID, Status: database.StatusPending}, nil
		},
		GetSubRideByRideAndPassengerFunc: func(ctx context.Context, rID, pID string) (*database.SubRide, error) {
			return &database.SubRide{SubRideID: subRideID, RideID: rideID, PassengerID: passengerID, Status: database.StatusPending}, nil
		},
		CancelSubRideIfActiveFunc: func(ctx context.Context, id string) (bool, error) {
			return false, errors.New("boom")
		},
	}
	sender := &mockSender{}
	h := &Handler{db: db, producer: sender}

	h.handleCancel(context.Background(), InboundMessage{
		Action: ActionCancel, RideID: rideID, UserID: passengerID,
	})

	if firstSubRideCancelledFor(sender, driverID) != nil {
		t.Error("driver MUST NOT receive sub_ride_cancelled when DB cancel failed")
	}
	if firstSubRideCancelledFor(sender, passengerID) != nil {
		t.Error("passenger MUST NOT receive sub_ride_cancelled when DB cancel failed")
	}
	errEvt := firstNotificationByEvent(sender, "ride_error")
	if errEvt == nil || errEvt.Message != "internal error" {
		t.Fatalf("expected 'internal error' ride_error, got %+v", errEvt)
	}
}

func TestHandleCancel_Stranger_Rejected(t *testing.T) {
	const rideID = "ride-1"
	const driverID = "driver-1"

	db := &handlerMockDB{
		GetRideFunc: func(ctx context.Context, id string) (*database.Ride, error) {
			return &database.Ride{RideID: rideID, DriverID: driverID, Status: database.StatusPending}, nil
		},
		GetSubRideByRideAndPassengerFunc: func(ctx context.Context, rID, pID string) (*database.SubRide, error) {
			return nil, sql.ErrNoRows
		},
	}
	sender := &mockSender{}
	h := &Handler{db: db, producer: sender}

	h.handleCancel(context.Background(), InboundMessage{
		Action: ActionCancel, RideID: rideID, UserID: "stranger-1",
	})

	errEvt := firstNotificationByEvent(sender, "ride_error")
	if errEvt == nil {
		t.Fatal("expected ride_error notification")
	}
	if errEvt.UserID != "stranger-1" {
		t.Errorf("expected ride_error to stranger-1, got %s", errEvt.UserID)
	}
	if errEvt.Message != "not a participant in this ride" {
		t.Errorf("unexpected error message: %s", errEvt.Message)
	}
	if firstSubRideCancelledFor(sender, driverID) != nil {
		t.Errorf("no sub_ride_cancelled should fire for a stranger")
	}
}

func TestHandleCancel_Passenger_ReasonPropagation_OmitemptyOnPassengerCopy(t *testing.T) {
	const rideID = "ride-1"
	const driverID = "driver-1"
	const passengerID = "pass-1"
	const subRideID = "sub-1"
	const reason = "took a different ride"

	db := &handlerMockDB{
		GetRideFunc: func(ctx context.Context, id string) (*database.Ride, error) {
			return &database.Ride{RideID: rideID, DriverID: driverID, Status: database.StatusPending}, nil
		},
		GetSubRideByRideAndPassengerFunc: func(ctx context.Context, rID, pID string) (*database.SubRide, error) {
			return &database.SubRide{SubRideID: subRideID, RideID: rideID, PassengerID: passengerID, Status: database.StatusPending}, nil
		},
		CancelSubRideIfActiveFunc: func(ctx context.Context, id string) (bool, error) { return true, nil },
	}
	sender := &mockSender{}
	h := &Handler{db: db, producer: sender}

	h.handleCancel(context.Background(), InboundMessage{
		Action: ActionCancel, RideID: rideID, UserID: passengerID, Reason: reason,
	})

	// Driver-bound: must carry reason AND passenger_id.
	driverEvt := firstSubRideCancelledFor(sender, driverID)
	if driverEvt == nil {
		t.Fatal("expected driver-bound sub_ride_cancelled")
	}
	if driverEvt.Reason != reason {
		t.Errorf("driver event reason: want %q, got %q", reason, driverEvt.Reason)
	}
	if driverEvt.PassengerID != passengerID {
		t.Errorf("driver event passenger_id: want %s, got %s", passengerID, driverEvt.PassengerID)
	}

	// Passenger-bound: Go struct must have zero-value Reason and PassengerID,
	// AND when marshalled to JSON omitempty must omit both keys entirely.
	passEvt := firstSubRideCancelledFor(sender, passengerID)
	if passEvt == nil {
		t.Fatal("expected passenger-bound sub_ride_cancelled")
	}
	if passEvt.Reason != "" {
		t.Errorf("passenger event Reason must be zero-value, got %q", passEvt.Reason)
	}
	if passEvt.PassengerID != "" {
		t.Errorf("passenger event PassengerID must be zero-value, got %q", passEvt.PassengerID)
	}
	raw, err := json.Marshal(passEvt)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	asMap := map[string]any{}
	if err := json.Unmarshal(raw, &asMap); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, present := asMap["reason"]; present {
		t.Errorf("passenger-bound JSON must not contain \"reason\" key, got: %s", string(raw))
	}
	if _, present := asMap["passenger_id"]; present {
		t.Errorf("passenger-bound JSON must not contain \"passenger_id\" key, got: %s", string(raw))
	}
}
