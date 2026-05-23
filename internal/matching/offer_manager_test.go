package matching

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
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
	calls     atomic.Int32
}

func (m *mockLocation) GetCurrentLocation(_ context.Context, userID string) (*location.Location, error) {
	m.calls.Add(1)
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
	getRideStatusCalls     atomic.Int32
	getDriverFractionCalls atomic.Int32
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
	m.getDriverFractionCalls.Add(1)
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
	m.getRideStatusCalls.Add(1)
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

// buildTestOM builds an OfferManager with the given mocks for filter-gate tests.
// Each test registers a t.Cleanup hook that stops any lingering offer/overall
// timers — the filter-gate tests are the first in this file that actually
// create real timers via offerToNextDriver, so they need the cleanup to avoid
// leaving background goroutines running past the test's lifetime.
func buildTestOM(t *testing.T, db *mockDB, sender *mockSender, loc *mockLocation) *OfferManager {
	om := NewOfferManager(sender, db, loc, defaultParams())
	t.Cleanup(func() {
		om.mu.Lock()
		defer om.mu.Unlock()
		ids := make([]string, 0, len(om.pending))
		for id := range om.pending {
			ids = append(ids, id)
		}
		for _, id := range ids {
			om.cleanup(id)
		}
	})
	return om
}

// testCandidate returns a MatchResult with reasonable defaults that can be
// tweaked per test.
func testCandidate(pickupFrac float64) database.MatchResult {
	return database.MatchResult{
		RideID:          "ride-1",
		DriverID:        "drv-1",
		RouteID:         "route-1",
		TotalDuration:   600,
		MaxPassengers:   3,
		Status:          database.StatusPending, // snapshot value; filter should refetch
		PickupFrac:      pickupFrac,
		DropoffFrac:     pickupFrac + 0.2,
		PickupPointLat:  43.24,
		PickupPointLon:  76.94,
		DropoffPointLat: 43.25,
		DropoffPointLon: 76.93,
	}
}

// testRideRequest returns a minimal InboundMessage for HandleRideRequest.
func testRideRequest() InboundMessage {
	return InboundMessage{
		RequestID:   "req-1",
		PassengerID: "pass-1",
		Pickup:      &InboundCoord{Latitude: 43.238, Longitude: 76.945},
		Dropoff:     &InboundCoord{Latitude: 43.255, Longitude: 76.930},
	}
}

func TestOfferManager_FilterGate_PendingBehindPickup(t *testing.T) {
	cand := testCandidate(0.5)
	db := &mockDB{
		FindMatchingRidesFunc: func(context.Context, database.MatchParams) ([]database.MatchResult, error) {
			return []database.MatchResult{cand}, nil
		},
		CountActiveSubRidesFunc: func(context.Context, string) (int, error) { return 0, nil },
		GetRideStatusFunc:       func(context.Context, string) (string, error) { return database.StatusPending, nil },
	}
	sender := &mockSender{}
	loc := &mockLocation{locations: map[string]*location.Location{}}

	om := buildTestOM(t, db, sender, loc)
	om.HandleRideRequest(context.Background(), testRideRequest())

	msgs := sender.messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	notif, ok := msgs[0].value.(NotificationMessage)
	if !ok {
		t.Fatalf("expected NotificationMessage, got %T", msgs[0].value)
	}
	if notif.Event != "ride_offer" {
		t.Errorf("expected ride_offer, got %s", notif.Event)
	}
	if db.getRideStatusCalls.Load() !=1 {
		t.Errorf("expected 1 GetRideStatus call, got %d", db.getRideStatusCalls.Load())
	}
	if db.getDriverFractionCalls.Load() !=0 {
		t.Errorf("expected 0 GetDriverFraction calls, got %d", db.getDriverFractionCalls.Load())
	}
	if loc.calls.Load() !=0 {
		t.Errorf("expected 0 GetCurrentLocation calls, got %d", loc.calls.Load())
	}
}

func TestOfferManager_FilterGate_PendingPastPickup(t *testing.T) {
	// Core behavior change: pending rides skip the position filter entirely,
	// so the driver being "past" the pickup along the route doesn't reject the offer.
	cand := testCandidate(0.3)
	db := &mockDB{
		FindMatchingRidesFunc: func(context.Context, database.MatchParams) ([]database.MatchResult, error) {
			return []database.MatchResult{cand}, nil
		},
		CountActiveSubRidesFunc: func(context.Context, string) (int, error) { return 0, nil },
		GetRideStatusFunc:       func(context.Context, string) (string, error) { return database.StatusPending, nil },
		// GetDriverFractionFunc intentionally unset — it must never be called.
	}
	sender := &mockSender{}
	loc := &mockLocation{
		locations: map[string]*location.Location{
			"drv-1": {Lat: 43.24, Lon: 76.94},
		},
	}

	om := buildTestOM(t, db, sender, loc)
	om.HandleRideRequest(context.Background(), testRideRequest())

	msgs := sender.messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	notif, ok := msgs[0].value.(NotificationMessage)
	if !ok || notif.Event != "ride_offer" {
		t.Fatalf("expected ride_offer, got %+v", msgs[0].value)
	}
	if db.getRideStatusCalls.Load() !=1 {
		t.Errorf("expected 1 GetRideStatus call, got %d", db.getRideStatusCalls.Load())
	}
	if db.getDriverFractionCalls.Load() !=0 {
		t.Errorf("expected 0 GetDriverFraction calls (filter must short-circuit for pending), got %d", db.getDriverFractionCalls.Load())
	}
	if loc.calls.Load() !=0 {
		t.Errorf("expected 0 GetCurrentLocation calls, got %d", loc.calls.Load())
	}
}

func TestOfferManager_FilterGate_ActiveBehindPickup(t *testing.T) {
	cand := testCandidate(0.5)
	db := &mockDB{
		FindMatchingRidesFunc: func(context.Context, database.MatchParams) ([]database.MatchResult, error) {
			return []database.MatchResult{cand}, nil
		},
		CountActiveSubRidesFunc: func(context.Context, string) (int, error) { return 0, nil },
		GetRideStatusFunc:       func(context.Context, string) (string, error) { return database.StatusActive, nil },
		GetDriverFractionFunc: func(context.Context, string, float64, float64) (float64, error) {
			return 0.2, nil // below PickupFrac (0.5) — filter doesn't fire
		},
	}
	sender := &mockSender{}
	loc := &mockLocation{
		locations: map[string]*location.Location{
			"drv-1": {Lat: 43.24, Lon: 76.94},
		},
	}

	om := buildTestOM(t, db, sender, loc)
	om.HandleRideRequest(context.Background(), testRideRequest())

	msgs := sender.messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	notif, ok := msgs[0].value.(NotificationMessage)
	if !ok || notif.Event != "ride_offer" {
		t.Fatalf("expected ride_offer, got %+v", msgs[0].value)
	}
	if db.getRideStatusCalls.Load() !=1 {
		t.Errorf("expected 1 GetRideStatus call, got %d", db.getRideStatusCalls.Load())
	}
	if db.getDriverFractionCalls.Load() !=1 {
		t.Errorf("expected 1 GetDriverFraction call, got %d", db.getDriverFractionCalls.Load())
	}
	if loc.calls.Load() !=1 {
		t.Errorf("expected 1 GetCurrentLocation call, got %d", loc.calls.Load())
	}
}

func TestOfferManager_FilterGate_ActivePastPickup(t *testing.T) {
	// Filter fires: driver is past pickup on an active ride, candidate skipped,
	// loop exhausts to exactly one candidate, sendNoMatch emits ride_no_match.
	cand := testCandidate(0.3)
	db := &mockDB{
		FindMatchingRidesFunc: func(context.Context, database.MatchParams) ([]database.MatchResult, error) {
			return []database.MatchResult{cand}, nil
		},
		CountActiveSubRidesFunc: func(context.Context, string) (int, error) { return 0, nil },
		GetRideStatusFunc:       func(context.Context, string) (string, error) { return database.StatusActive, nil },
		GetDriverFractionFunc: func(context.Context, string, float64, float64) (float64, error) {
			return 0.8, nil // above PickupFrac (0.3) — filter fires
		},
	}
	sender := &mockSender{}
	loc := &mockLocation{
		locations: map[string]*location.Location{
			"drv-1": {Lat: 43.24, Lon: 76.94},
		},
	}

	om := buildTestOM(t, db, sender, loc)
	om.HandleRideRequest(context.Background(), testRideRequest())

	msgs := sender.messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	notif, ok := msgs[0].value.(NotificationMessage)
	if !ok {
		t.Fatalf("expected NotificationMessage, got %T", msgs[0].value)
	}
	if notif.Event != "ride_no_match" {
		t.Errorf("expected ride_no_match (filter dropped the only candidate), got %s", notif.Event)
	}
	// Two-assertion shape: distinguishes "filter dropped it" from "filter never reached."
	if db.getRideStatusCalls.Load() !=1 {
		t.Errorf("expected 1 GetRideStatus call, got %d", db.getRideStatusCalls.Load())
	}
	if db.getDriverFractionCalls.Load() !=1 {
		t.Errorf("expected 1 GetDriverFraction call (proves filter actually ran), got %d", db.getDriverFractionCalls.Load())
	}
}

func TestOfferManager_FilterGate_StatusErrorFailsOpen(t *testing.T) {
	cand := testCandidate(0.3)
	db := &mockDB{
		FindMatchingRidesFunc: func(context.Context, database.MatchParams) ([]database.MatchResult, error) {
			return []database.MatchResult{cand}, nil
		},
		CountActiveSubRidesFunc: func(context.Context, string) (int, error) { return 0, nil },
		GetRideStatusFunc: func(context.Context, string) (string, error) {
			return "", errors.New("boom")
		},
		// GetDriverFractionFunc intentionally unset — it must never be called.
	}
	sender := &mockSender{}
	loc := &mockLocation{locations: map[string]*location.Location{}}

	om := buildTestOM(t, db, sender, loc)
	om.HandleRideRequest(context.Background(), testRideRequest())

	msgs := sender.messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	notif, ok := msgs[0].value.(NotificationMessage)
	if !ok || notif.Event != "ride_offer" {
		t.Fatalf("expected ride_offer (fail-open), got %+v", msgs[0].value)
	}
	if db.getRideStatusCalls.Load() !=1 {
		t.Errorf("expected 1 GetRideStatus call, got %d", db.getRideStatusCalls.Load())
	}
	if db.getDriverFractionCalls.Load() !=0 {
		t.Errorf("expected 0 GetDriverFraction calls (error branch skips the position check), got %d", db.getDriverFractionCalls.Load())
	}
	if loc.calls.Load() !=0 {
		t.Errorf("expected 0 GetCurrentLocation calls, got %d", loc.calls.Load())
	}
}

func TestHaversine(t *testing.T) {
	d := haversine(43.238, 76.945, 43.250, 76.945)
	if d < 1300 || d > 1400 {
		t.Errorf("expected ~1334m, got %.0fm", d)
	}
}

// findMessagesOfType returns all sent messages on TopicRideNotifications whose
// value has the concrete type T. Callers assert the Event field themselves on
// the returned slice — this helper filters by topic and Go type only.
func findMessagesOfType[T any](sender *mockSender) []T {
	out := []T{}
	for _, m := range sender.messages() {
		if m.topic != TopicRideNotifications {
			continue
		}
		if v, ok := m.value.(T); ok {
			out = append(out, v)
		}
	}
	return out
}

// acceptingDB returns a mockDB that stubs CreateSubRideWithCapacityCheck to
// succeed, assigning a fixed SubRideID — mirrors the Postgres RETURNING path.
func acceptingDB() *mockDB {
	return &mockDB{
		CreateSubRideWithCapacityCheckFunc: func(_ context.Context, sr *database.SubRide) error {
			sr.SubRideID = "sub-1"
			return nil
		},
	}
}

// primeAcceptedOffer seeds the OfferManager with a pending request whose
// CurrentOffer matches a known driver/ride, simulating the state right before
// HandleOfferResponse processes a driver accept.
func primeAcceptedOffer(om *OfferManager) {
	cand := testCandidate(0.3)
	om.mu.Lock()
	om.pending["req-1"] = &PendingRequest{
		RequestID:    "req-1",
		PassengerID:  "pass-1",
		PickupLat:    43.238,
		PickupLon:    76.945,
		DropoffLat:   43.255,
		DropoffLon:   76.930,
		CurrentOffer: &cand,
	}
	om.mu.Unlock()
}

func TestHandleOfferResponse_Accept_EmitsDriverRoute(t *testing.T) {
	sender := &mockSender{}
	loc := &mockLocation{locations: map[string]*location.Location{
		"pass-1": {Lat: 43.238, Lon: 76.945},
	}}
	db := acceptingDB()
	om := buildTestOM(t, db, sender, loc)
	primeAcceptedOffer(om)

	accepted := true
	om.HandleOfferResponse(context.Background(), InboundMessage{
		Action:    ActionOfferResponse,
		RequestID: "req-1",
		UserID:    "drv-1", // matches testCandidate's DriverID
		Accepted:  &accepted,
	})

	routes := findMessagesOfType[DriverRouteMessage](sender)
	if len(routes) != 1 {
		t.Fatalf("expected 1 DriverRouteMessage, got %d (all msgs: %+v)", len(routes), sender.messages())
	}
	got := routes[0]
	if got.Event != EventDriverRoute {
		t.Errorf("Event: want %q, got %q", EventDriverRoute, got.Event)
	}
	if got.UserID != "pass-1" {
		t.Errorf("UserID (recipient): want pass-1, got %q", got.UserID)
	}
	if got.RideID != "ride-1" {
		t.Errorf("RideID: want ride-1, got %q", got.RideID)
	}
	if got.SubRideID != "sub-1" {
		t.Errorf("SubRideID: want sub-1, got %q", got.SubRideID)
	}
	if got.DriverID != "drv-1" {
		t.Errorf("DriverID: want drv-1, got %q", got.DriverID)
	}
	if got.RouteID != "route-1" {
		t.Errorf("RouteID: want route-1, got %q", got.RouteID)
	}

	// Confirm the message is keyed by passenger_id (Kafka partitioning matches
	// the existing driver-assigned send so the same partition fans both events).
	for _, m := range sender.messages() {
		if _, ok := m.value.(DriverRouteMessage); ok {
			if m.key != "pass-1" {
				t.Errorf("driver_route key: want pass-1, got %q", m.key)
			}
		}
	}
}

func TestHandleOfferResponse_Accept_EmitsPassengerLocation(t *testing.T) {
	sender := &mockSender{}
	loc := &mockLocation{locations: map[string]*location.Location{
		"pass-1": {Lat: 43.2400, Lon: 76.9500},
	}}
	db := acceptingDB()
	om := buildTestOM(t, db, sender, loc)
	primeAcceptedOffer(om)

	accepted := true
	om.HandleOfferResponse(context.Background(), InboundMessage{
		Action:    ActionOfferResponse,
		RequestID: "req-1",
		UserID:    "drv-1",
		Accepted:  &accepted,
	})

	locs := findMessagesOfType[PassengerLocationMessage](sender)
	if len(locs) != 1 {
		t.Fatalf("expected 1 PassengerLocationMessage, got %d (all msgs: %+v)", len(locs), sender.messages())
	}
	got := locs[0]
	if got.Event != EventPassengerLocation {
		t.Errorf("Event: want %q, got %q", EventPassengerLocation, got.Event)
	}
	if got.UserID != "drv-1" {
		t.Errorf("UserID (recipient): want drv-1, got %q", got.UserID)
	}
	if got.RideID != "ride-1" {
		t.Errorf("RideID: want ride-1, got %q", got.RideID)
	}
	if got.SubRideID != "sub-1" {
		t.Errorf("SubRideID: want sub-1, got %q", got.SubRideID)
	}
	if got.PassengerID != "pass-1" {
		t.Errorf("PassengerID: want pass-1, got %q", got.PassengerID)
	}
	if got.Lat != 43.2400 {
		t.Errorf("Lat: want 43.2400, got %v", got.Lat)
	}
	if got.Lon != 76.9500 {
		t.Errorf("Lon: want 76.9500, got %v", got.Lon)
	}

	// Confirm Kafka key matches the recipient (driver) — same convention as
	// other ride-notifications direct-to-user sends.
	for _, m := range sender.messages() {
		if _, ok := m.value.(PassengerLocationMessage); ok {
			if m.key != "drv-1" {
				t.Errorf("passenger_location key: want drv-1, got %q", m.key)
			}
		}
	}

	// Sanity: GetCurrentLocation was actually invoked for the passenger.
	if loc.calls.Load() != 1 {
		t.Errorf("expected 1 GetCurrentLocation call, got %d", loc.calls.Load())
	}
}
