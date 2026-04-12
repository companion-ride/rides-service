package matching

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"rides/internal/config"
	"rides/internal/database"
	"rides/internal/location"
)

func defaultParams() *config.MatchingParamsLoader {
	return config.NewMatchingParamsLoader(&config.MatchingParams{
		PickupRadiusM:      500,
		DropoffRadiusM:     500,
		MaxPassengers:      3,
		MinETASeconds:      180,
		MinDriverDistanceM: 2000,
	})
}

// --- test helpers ---

type sentMessage struct {
	topic string
	key   string
	value any
}

type mockSender struct {
	mu   sync.Mutex
	sent []sentMessage
}

func (m *mockSender) Send(_ context.Context, topic, key string, value any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, sentMessage{topic, key, value})
	return nil
}

func (m *mockSender) messages() []sentMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]sentMessage, len(m.sent))
	copy(cp, m.sent)
	return cp
}

type mockLocation struct {
	locations map[string]*location.Location
	calls     int
}

func (m *mockLocation) GetCurrentLocation(_ context.Context, userID string) (*location.Location, error) {
	m.calls++
	if loc, ok := m.locations[userID]; ok {
		return loc, nil
	}
	return nil, context.DeadlineExceeded
}

type mockDB struct {
	FindMatchingRidesFunc              func(ctx context.Context, params database.MatchParams) ([]database.MatchResult, error)
	CountActiveSubRidesFunc            func(ctx context.Context, rideID string) (int, error)
	GetDriverFractionFunc              func(ctx context.Context, rideID string, lon, lat float64) (float64, error)
	CreateSubRideWithCapacityCheckFunc func(ctx context.Context, subRide *database.SubRide) error
	GetRideStatusFunc                  func(ctx context.Context, rideID string) (string, error)

	// Call counters for assertions. Incremented unconditionally at the top of
	// each method (before the stub check) so tests can assert "this method was
	// called at all" — not just "this method was called with the stub installed."
	getRideStatusCalls     int
	getDriverFractionCalls int
}

func (m *mockDB) FindMatchingRides(ctx context.Context, params database.MatchParams) ([]database.MatchResult, error) {
	if m.FindMatchingRidesFunc != nil {
		return m.FindMatchingRidesFunc(ctx, params)
	}
	return nil, errors.New("mockDB: FindMatchingRides not stubbed")
}

func (m *mockDB) CountActiveSubRides(ctx context.Context, rideID string) (int, error) {
	if m.CountActiveSubRidesFunc != nil {
		return m.CountActiveSubRidesFunc(ctx, rideID)
	}
	return 0, nil
}

func (m *mockDB) GetDriverFraction(ctx context.Context, rideID string, lon, lat float64) (float64, error) {
	m.getDriverFractionCalls++
	if m.GetDriverFractionFunc != nil {
		return m.GetDriverFractionFunc(ctx, rideID, lon, lat)
	}
	return 0, errors.New("mockDB: GetDriverFraction not stubbed")
}

func (m *mockDB) CreateSubRideWithCapacityCheck(ctx context.Context, subRide *database.SubRide) error {
	if m.CreateSubRideWithCapacityCheckFunc != nil {
		return m.CreateSubRideWithCapacityCheckFunc(ctx, subRide)
	}
	return errors.New("mockDB: CreateSubRideWithCapacityCheck not stubbed")
}

func (m *mockDB) GetRideStatus(ctx context.Context, rideID string) (string, error) {
	m.getRideStatusCalls++
	if m.GetRideStatusFunc != nil {
		return m.GetRideStatusFunc(ctx, rideID)
	}
	return "", errors.New("mockDB: GetRideStatus not stubbed")
}

// Compile-time assertion that mockDB satisfies the matching.DB interface.
var _ DB = (*mockDB)(nil)

func TestOfferManager_CleanupStopsTimers(t *testing.T) {
	om := &OfferManager{
		pending: make(map[string]*PendingRequest),
	}
	fired := false
	req := &PendingRequest{
		RequestID:    "req-1",
		PassengerID:  "pass-1",
		OfferTimer:   time.AfterFunc(time.Hour, func() { fired = true }),
		OverallTimer: time.AfterFunc(time.Hour, func() { fired = true }),
	}
	om.pending["req-1"] = req

	om.cleanup("req-1")

	if _, ok := om.pending["req-1"]; ok {
		t.Error("pending request should be removed after cleanup")
	}
	time.Sleep(10 * time.Millisecond)
	if fired {
		t.Error("timers should have been stopped")
	}
}

func TestOfferManager_NoMatchEmptyCandidates(t *testing.T) {
	sender := &mockSender{}
	om := &OfferManager{
		pending:  make(map[string]*PendingRequest),
		producer: sender,
		params:   defaultParams(),
	}
	om.pending["req-1"] = &PendingRequest{
		RequestID:   "req-1",
		PassengerID: "pass-1",
		Candidates:  []database.MatchResult{},
		CurrentIdx:  0,
	}

	om.offerToNextDriver(context.Background(), "req-1")

	msgs := sender.messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].topic != TopicRideNotifications {
		t.Errorf("expected topic %s, got %s", TopicRideNotifications, msgs[0].topic)
	}
	notif, ok := msgs[0].value.(NotificationMessage)
	if !ok {
		t.Fatal("expected NotificationMessage")
	}
	if notif.Event != "ride_no_match" {
		t.Errorf("expected event ride_no_match, got %s", notif.Event)
	}
	if notif.UserID != "pass-1" {
		t.Errorf("expected user_id pass-1, got %s", notif.UserID)
	}
	if _, ok := om.pending["req-1"]; ok {
		t.Error("pending request should be cleaned up after no match")
	}
}

func TestHaversine(t *testing.T) {
	d := haversine(43.238, 76.945, 43.250, 76.945)
	if d < 1300 || d > 1400 {
		t.Errorf("expected ~1334m, got %.0fm", d)
	}
}
