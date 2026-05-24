package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"rides/internal/config"
	"rides/internal/database"
	"rides/internal/pathfinder"
)

// --- fakes ---

type fakePathfinder struct {
	resp            *pathfinder.RouteResponse
	err             error
	gotSegments     []pathfinder.SegmentInput
	createCustomHit bool
}

func (f *fakePathfinder) CreateRoute(ctx context.Context, auth string, sLat, sLon, fLat, fLon float64) (*pathfinder.RouteResponse, error) {
	return nil, nil
}
func (f *fakePathfinder) CreateCustomRoute(ctx context.Context, auth string, segs []pathfinder.SegmentInput) (*pathfinder.RouteResponse, error) {
	f.createCustomHit = true
	f.gotSegments = segs
	return f.resp, f.err
}
func (f *fakePathfinder) GetRoute(ctx context.Context, auth, id string) (*pathfinder.RouteResponse, error) {
	return nil, nil
}
func (f *fakePathfinder) WalkToClosest(ctx context.Context, auth, id string, lat, lon float64) (*pathfinder.WalkResponse, error) {
	return nil, nil
}

type fakeDB struct {
	database.Service
	created     *database.Ride
	createErr   error
	createCalls int
}

func (f *fakeDB) CreateRide(ctx context.Context, r *database.Ride) error {
	f.createCalls++
	if f.createErr != nil {
		return f.createErr
	}
	r.RideID = "ride-123"
	f.created = r
	return nil
}

const testUserID = "11111111-1111-1111-1111-111111111111"

func newCustomTestServer(pf pathfinderClient, db database.Service) *Server {
	return &Server{
		pathfinder:     pf,
		db:             db,
		matchingParams: config.NewMatchingParamsLoader(config.DefaultMatchingParams()),
	}
}

func doCustomPath(t *testing.T, s *Server, userID, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/ride/driver/custom-path", func(c *gin.Context) {
		c.Set("user_id", userID)
		s.CreateDriverCustomPathHandler(c)
	})
	req := httptest.NewRequest(http.MethodPost, "/ride/driver/custom-path", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func cannedRouteResp() *pathfinder.RouteResponse {
	return &pathfinder.RouteResponse{
		RouteID: "route-abc",
		Segments: []pathfinder.Segment{
			{Coords: []pathfinder.Coord{{Lat: 51.10, Lon: 71.40}, {Lat: 51.11, Lon: 71.41}}, Distance: 1200, Duration: 108},
			{Coords: []pathfinder.Coord{{Lat: 51.11, Lon: 71.41}, {Lat: 51.12, Lon: 71.42}}, Distance: 1200, Duration: 108},
		},
		TotalDistance: "2.4 km",
		TotalDuration: "3 min",
	}
}

func TestCustomPath_EmptySegments(t *testing.T) {
	s := newCustomTestServer(&fakePathfinder{}, &fakeDB{})
	body := `{"driver_id":"` + testUserID + `","segments":[]}`
	rr := doCustomPath(t, s, testUserID, body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestCustomPath_SegmentTooFewCoords(t *testing.T) {
	s := newCustomTestServer(&fakePathfinder{}, &fakeDB{})
	body := `{"driver_id":"` + testUserID + `","segments":[{"coords":[{"lat":51.1,"lon":71.4}]}]}`
	rr := doCustomPath(t, s, testUserID, body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestCustomPath_TooManyCoords(t *testing.T) {
	s := newCustomTestServer(&fakePathfinder{}, &fakeDB{})
	var sb strings.Builder
	sb.WriteString(`{"driver_id":"` + testUserID + `","segments":[{"coords":[`)
	for i := 0; i < 501; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"lat":51.1,"lon":71.4}`)
	}
	sb.WriteString(`]}]}`)
	rr := doCustomPath(t, s, testUserID, sb.String())
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestCustomPath_LatOutOfRange(t *testing.T) {
	s := newCustomTestServer(&fakePathfinder{}, &fakeDB{})
	body := `{"driver_id":"` + testUserID + `","segments":[{"coords":[{"lat":91,"lon":71.4},{"lat":51.1,"lon":71.4}]}]}`
	rr := doCustomPath(t, s, testUserID, body)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestCustomPath_LatZeroAccepted(t *testing.T) {
	pf := &fakePathfinder{resp: cannedRouteResp()}
	db := &fakeDB{}
	s := newCustomTestServer(pf, db)
	body := `{"driver_id":"` + testUserID + `","segments":[{"coords":[{"lat":0,"lon":0},{"lat":0.001,"lon":0}]}]}`
	rr := doCustomPath(t, s, testUserID, body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201; body=%s", rr.Code, rr.Body.String())
	}
}

func TestCustomPath_DriverIDMismatch(t *testing.T) {
	s := newCustomTestServer(&fakePathfinder{}, &fakeDB{})
	body := `{"driver_id":"22222222-2222-2222-2222-222222222222","segments":[{"coords":[{"lat":51.10,"lon":71.40},{"lat":51.11,"lon":71.41}]}]}`
	rr := doCustomPath(t, s, testUserID, body)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403; body=%s", rr.Code, rr.Body.String())
	}
}

func TestCustomPath_HappyPath(t *testing.T) {
	pf := &fakePathfinder{resp: cannedRouteResp()}
	db := &fakeDB{}
	s := newCustomTestServer(pf, db)
	body := `{"driver_id":"` + testUserID + `","segments":[
		{"coords":[{"lat":51.10,"lon":71.40},{"lat":51.11,"lon":71.41}]},
		{"coords":[{"lat":51.11,"lon":71.41},{"lat":51.12,"lon":71.42}]}
	],"max_passengers":2}`
	rr := doCustomPath(t, s, testUserID, body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201; body=%s", rr.Code, rr.Body.String())
	}

	if !pf.createCustomHit {
		t.Error("expected CreateCustomRoute to be called")
	}
	if len(pf.gotSegments) != 2 || len(pf.gotSegments[0].Coords) != 2 {
		t.Errorf("forwarded segments wrong shape: %+v", pf.gotSegments)
	}
	if db.created.StartLat != 51.10 || db.created.StartLon != 71.40 {
		t.Errorf("start = %v,%v want 51.10,71.40", db.created.StartLat, db.created.StartLon)
	}
	if db.created.FinishLat != 51.12 || db.created.FinishLon != 71.42 {
		t.Errorf("finish = %v,%v want 51.12,71.42", db.created.FinishLat, db.created.FinishLon)
	}
	if db.created.RouteID != "route-abc" {
		t.Errorf("route_id = %q", db.created.RouteID)
	}
	if db.created.MaxPassengers != 2 {
		t.Errorf("max_passengers = %d want 2", db.created.MaxPassengers)
	}
	if !strings.HasPrefix(db.created.GeometryWKT, "LINESTRING(") {
		t.Errorf("geometry = %q", db.created.GeometryWKT)
	}

	var resp CreateDriverPathResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.RideID != "ride-123" || resp.RouteID != "route-abc" || resp.Status != database.StatusPending {
		t.Errorf("resp = %+v", resp)
	}
}

func TestCustomPath_ClosedLoop(t *testing.T) {
	pf := &fakePathfinder{resp: &pathfinder.RouteResponse{
		RouteID: "route-loop",
		Segments: []pathfinder.Segment{
			{Coords: []pathfinder.Coord{{Lat: 51.10, Lon: 71.40}, {Lat: 51.11, Lon: 71.41}, {Lat: 51.10, Lon: 71.40}}},
		},
		TotalDuration: "2 min",
	}}
	db := &fakeDB{}
	s := newCustomTestServer(pf, db)
	body := `{"driver_id":"` + testUserID + `","segments":[{"coords":[{"lat":51.10,"lon":71.40},{"lat":51.11,"lon":71.41},{"lat":51.10,"lon":71.40}]}]}`
	rr := doCustomPath(t, s, testUserID, body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201; body=%s", rr.Code, rr.Body.String())
	}
	if db.created.StartLat != db.created.FinishLat || db.created.StartLon != db.created.FinishLon {
		t.Errorf("closed loop should have start==finish, got %v vs %v",
			[]float64{db.created.StartLat, db.created.StartLon},
			[]float64{db.created.FinishLat, db.created.FinishLon})
	}
}

func TestCustomPath_PathfinderError(t *testing.T) {
	pf := &fakePathfinder{err: errTest}
	db := &fakeDB{}
	s := newCustomTestServer(pf, db)
	body := `{"driver_id":"` + testUserID + `","segments":[{"coords":[{"lat":51.10,"lon":71.40},{"lat":51.11,"lon":71.41}]}]}`
	rr := doCustomPath(t, s, testUserID, body)
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("got %d, want 502; body=%s", rr.Code, rr.Body.String())
	}
	if db.createCalls != 0 {
		t.Error("CreateRide should not be called when pathfinder fails")
	}
}

func TestCustomPath_OrphanedRouteOnDBFailure(t *testing.T) {
	pf := &fakePathfinder{resp: cannedRouteResp()}
	db := &fakeDB{createErr: errTest}
	s := newCustomTestServer(pf, db)
	body := `{"driver_id":"` + testUserID + `","segments":[{"coords":[{"lat":51.10,"lon":71.40},{"lat":51.11,"lon":71.41}]}]}`
	rr := doCustomPath(t, s, testUserID, body)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500; body=%s", rr.Code, rr.Body.String())
	}
}

func TestCustomPath_MaxPassengersDefault(t *testing.T) {
	pf := &fakePathfinder{resp: cannedRouteResp()}
	db := &fakeDB{}
	s := newCustomTestServer(pf, db)
	// max_passengers omitted → defaults to 3 (DefaultMatchingParams ceiling).
	body := `{"driver_id":"` + testUserID + `","segments":[{"coords":[{"lat":51.10,"lon":71.40},{"lat":51.11,"lon":71.41}]}]}`
	rr := doCustomPath(t, s, testUserID, body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201; body=%s", rr.Code, rr.Body.String())
	}
	if db.created.MaxPassengers != 3 {
		t.Errorf("max_passengers default = %d, want 3", db.created.MaxPassengers)
	}
}

func TestCustomPath_MaxPassengersClampedToCeiling(t *testing.T) {
	pf := &fakePathfinder{resp: cannedRouteResp()}
	db := &fakeDB{}
	s := newCustomTestServer(pf, db)
	// max_passengers=10 exceeds the DefaultMatchingParams ceiling (3) → clamped.
	body := `{"driver_id":"` + testUserID + `","segments":[{"coords":[{"lat":51.10,"lon":71.40},{"lat":51.11,"lon":71.41}]}],"max_passengers":10}`
	rr := doCustomPath(t, s, testUserID, body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201; body=%s", rr.Code, rr.Body.String())
	}
	if db.created.MaxPassengers != 3 {
		t.Errorf("max_passengers clamp = %d, want 3", db.created.MaxPassengers)
	}
}

func TestCustomPath_EmptyPathfinderGeometry(t *testing.T) {
	// Pathfinder returns no segments → segmentsToWKT yields "" → 500, no ride saved.
	pf := &fakePathfinder{resp: &pathfinder.RouteResponse{RouteID: "route-empty", Segments: nil}}
	db := &fakeDB{}
	s := newCustomTestServer(pf, db)
	body := `{"driver_id":"` + testUserID + `","segments":[{"coords":[{"lat":51.10,"lon":71.40},{"lat":51.11,"lon":71.41}]}]}`
	rr := doCustomPath(t, s, testUserID, body)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500; body=%s", rr.Code, rr.Body.String())
	}
	if db.createCalls != 0 {
		t.Error("CreateRide should not be called when geometry is empty")
	}
}

var errTest = &testError{}

type testError struct{}

func (e *testError) Error() string { return "test error" }
