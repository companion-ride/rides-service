//go:build integration

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"rides/internal/config"
	"rides/internal/database"
	"rides/internal/pathfinder"
)

func stubPathfinder(t *testing.T, status int, resp *pathfinder.RouteResponse) *pathfinder.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/routes/custom") {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if resp != nil {
			_ = json.NewEncoder(w).Encode(resp)
		}
	}))
	t.Cleanup(srv.Close)
	return pathfinder.NewClient(srv.URL)
}

func TestIntegration_CustomPath_HappyPath(t *testing.T) {
	ctx := context.Background()
	db := mustNewIntegrationDB(t, ctx)

	pf := stubPathfinder(t, http.StatusCreated, &pathfinder.RouteResponse{
		RouteID: "00000000-0000-0000-0000-0000000000aa",
		Segments: []pathfinder.Segment{
			{Coords: []pathfinder.Coord{{Lat: 51.10, Lon: 71.40}, {Lat: 51.11, Lon: 71.41}}},
			{Coords: []pathfinder.Coord{{Lat: 51.11, Lon: 71.41}, {Lat: 51.12, Lon: 71.42}}},
		},
		TotalDuration: "3 min",
	})

	s := &Server{
		pathfinder:     pf,
		db:             db,
		matchingParams: config.NewMatchingParamsLoader(config.DefaultMatchingParams()),
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	const uid = "11111111-1111-1111-1111-111111111111"
	r.POST("/ride/driver/custom-path", func(c *gin.Context) {
		c.Set("user_id", uid)
		s.CreateDriverCustomPathHandler(c)
	})

	body := `{"driver_id":"` + uid + `","segments":[
		{"coords":[{"lat":51.10,"lon":71.40},{"lat":51.11,"lon":71.41}]},
		{"coords":[{"lat":51.11,"lon":71.41},{"lat":51.12,"lon":71.42}]}
	]}`
	req := httptest.NewRequest(http.MethodPost, "/ride/driver/custom-path", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201; body=%s", rr.Code, rr.Body.String())
	}
	var resp CreateDriverPathResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.RouteID != "00000000-0000-0000-0000-0000000000aa" {
		t.Errorf("route_id = %q", resp.RouteID)
	}

	ride, err := db.GetRide(ctx, resp.RideID)
	if err != nil {
		t.Fatalf("read back ride: %v", err)
	}
	if ride.StartLat != 51.10 || ride.FinishLat != 51.12 {
		t.Errorf("start/finish lat = %v/%v want 51.10/51.12", ride.StartLat, ride.FinishLat)
	}
	if !strings.HasPrefix(ride.GeometryWKT, "LINESTRING(") {
		t.Errorf("geometry = %q", ride.GeometryWKT)
	}
}

func TestIntegration_CustomPath_PathfinderBadGateway(t *testing.T) {
	ctx := context.Background()
	db := mustNewIntegrationDB(t, ctx)
	pf := stubPathfinder(t, http.StatusInternalServerError, nil)

	s := &Server{
		pathfinder:     pf,
		db:             db,
		matchingParams: config.NewMatchingParamsLoader(config.DefaultMatchingParams()),
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	const uid = "11111111-1111-1111-1111-111111111111"
	r.POST("/ride/driver/custom-path", func(c *gin.Context) {
		c.Set("user_id", uid)
		s.CreateDriverCustomPathHandler(c)
	})

	body := `{"driver_id":"` + uid + `","segments":[{"coords":[{"lat":51.10,"lon":71.40},{"lat":51.11,"lon":71.41}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/ride/driver/custom-path", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("got %d, want 502; body=%s", rr.Code, rr.Body.String())
	}
}

var testConnStr string

func TestMain(m *testing.M) {
	dbName, dbPwd, dbUser := "database", "password", "user"
	ctr, err := postgres.Run(
		context.Background(),
		"postgis/postgis:17-3.5",
		postgres.WithDatabase(dbName),
		postgres.WithUsername(dbUser),
		postgres.WithPassword(dbPwd),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second)),
	)
	if err != nil {
		log.Fatalf("start postgres: %v", err)
	}
	host, err := ctr.Host(context.Background())
	if err != nil {
		log.Fatalf("container host: %v", err)
	}
	port, err := ctr.MappedPort(context.Background(), "5432/tcp")
	if err != nil {
		log.Fatalf("container port: %v", err)
	}
	testConnStr = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		dbUser, dbPwd, host, port.Port(), dbName)

	if err := database.RunMigrations(testConnStr, "../../migrations"); err != nil {
		log.Fatalf("run migrations: %v", err)
	}

	code := m.Run()
	_ = ctr.Terminate(context.Background())
	os.Exit(code)
}

func mustNewIntegrationDB(t *testing.T, _ context.Context) database.Service {
	t.Helper()
	return database.New(testConnStr)
}
