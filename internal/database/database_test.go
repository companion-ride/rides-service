package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var testConnStr string

func mustStartPostgresContainer() (func(context.Context, ...testcontainers.TerminateOption) error, error) {
	var (
		dbName = "database"
		dbPwd  = "password"
		dbUser = "user"
	)

	dbContainer, err := postgres.Run(
		context.Background(),
		"postgis/postgis:17-3.5",
		postgres.WithDatabase(dbName),
		postgres.WithUsername(dbUser),
		postgres.WithPassword(dbPwd),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(5*time.Second)),
	)
	if err != nil {
		return nil, err
	}

	dbHost, err := dbContainer.Host(context.Background())
	if err != nil {
		return dbContainer.Terminate, err
	}

	dbPort, err := dbContainer.MappedPort(context.Background(), "5432/tcp")
	if err != nil {
		return dbContainer.Terminate, err
	}

	testConnStr = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		dbUser, dbPwd, dbHost, dbPort.Port(), dbName)

	return dbContainer.Terminate, err
}

func TestMain(m *testing.M) {
	teardown, err := mustStartPostgresContainer()
	if err != nil {
		log.Fatalf("could not start postgres container: %v", err)
	}

	if err := RunMigrations(testConnStr, "../../migrations"); err != nil {
		log.Fatalf("could not run migrations: %v", err)
	}

	m.Run()

	if teardown != nil && teardown(context.Background()) != nil {
		log.Fatalf("could not teardown postgres container: %v", err)
	}
}

func TestNew(t *testing.T) {
	dbOnce = sync.Once{} // reset singleton
	dbInstance = nil
	srv := New(testConnStr)
	if srv == nil {
		t.Fatal("New() returned nil")
	}
}

func TestHealth(t *testing.T) {
	dbOnce = sync.Once{}
	dbInstance = nil
	srv := New(testConnStr)

	stats := srv.Health()

	if stats["status"] != "up" {
		t.Fatalf("expected status to be up, got %s", stats["status"])
	}

	if _, ok := stats["error"]; ok {
		t.Fatalf("expected error not to be present")
	}

	if stats["message"] != "It's healthy" {
		t.Fatalf("expected message to be 'It's healthy', got %s", stats["message"])
	}
}

func TestClose(t *testing.T) {
	dbOnce = sync.Once{}
	dbInstance = nil
	srv := New(testConnStr)

	if srv.Close() != nil {
		t.Fatalf("expected Close() to return nil")
	}
}

func TestGetRideStatus(t *testing.T) {
	dbOnce = sync.Once{} // reset singleton
	dbInstance = nil
	srv := New(testConnStr)
	ctx := context.Background()

	t.Run("pending ride", func(t *testing.T) {
		ride := &Ride{
			DriverID:      "00000000-0000-0000-0000-000000000001",
			RouteID:       "00000000-0000-0000-0000-000000000002",
			GeometryWKT:   "LINESTRING(76.94 43.24, 76.95 43.25)",
			StartLat:      43.24,
			StartLon:      76.94,
			FinishLat:     43.25,
			FinishLon:     76.95,
			TotalDuration: 600,
			MaxPassengers: 3,
			Status:        StatusPending,
		}
		if err := srv.CreateRide(ctx, ride); err != nil {
			t.Fatalf("CreateRide: %v", err)
		}

		status, err := srv.GetRideStatus(ctx, ride.RideID)
		if err != nil {
			t.Fatalf("GetRideStatus: %v", err)
		}
		if status != StatusPending {
			t.Errorf("expected %q, got %q", StatusPending, status)
		}
	})

	t.Run("active ride", func(t *testing.T) {
		ride := &Ride{
			DriverID:      "00000000-0000-0000-0000-000000000003",
			RouteID:       "00000000-0000-0000-0000-000000000004",
			GeometryWKT:   "LINESTRING(76.94 43.24, 76.95 43.25)",
			StartLat:      43.24,
			StartLon:      76.94,
			FinishLat:     43.25,
			FinishLon:     76.95,
			TotalDuration: 600,
			MaxPassengers: 3,
			Status:        StatusPending,
		}
		if err := srv.CreateRide(ctx, ride); err != nil {
			t.Fatalf("CreateRide: %v", err)
		}
		if err := srv.UpdateRideStatus(ctx, ride.RideID, StatusActive); err != nil {
			t.Fatalf("UpdateRideStatus: %v", err)
		}

		status, err := srv.GetRideStatus(ctx, ride.RideID)
		if err != nil {
			t.Fatalf("GetRideStatus: %v", err)
		}
		if status != StatusActive {
			t.Errorf("expected %q, got %q", StatusActive, status)
		}
	})

	t.Run("missing ride returns sql.ErrNoRows", func(t *testing.T) {
		missing := "00000000-0000-0000-0000-0000000000ff"
		_, err := srv.GetRideStatus(ctx, missing)
		if err == nil {
			t.Fatal("expected error for missing ride, got nil")
		}
		if !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("expected sql.ErrNoRows, got %v", err)
		}
	})
}

func TestGetSubRideByRideAndPassenger_Found(t *testing.T) {
	dbOnce = sync.Once{}
	dbInstance = nil
	srv := New(testConnStr)
	ctx := context.Background()

	if _, err := srv.DeleteAllRides(ctx); err != nil {
		t.Fatalf("clean: %v", err)
	}

	ride := &Ride{
		DriverID:      "11111111-1111-1111-1111-111111111111",
		RouteID:       "22222222-2222-2222-2222-222222222222",
		GeometryWKT:   "LINESTRING(71.40 51.10, 71.45 51.15)",
		StartLat:      51.10, StartLon: 71.40,
		FinishLat:     51.15, FinishLon: 71.45,
		TotalDuration: 600,
		MaxPassengers: 3,
		Status:        StatusPending,
	}
	if err := srv.CreateRide(ctx, ride); err != nil {
		t.Fatalf("CreateRide: %v", err)
	}

	srA := &SubRide{
		RideID:      ride.RideID,
		PassengerID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		PickupLat:   51.11, PickupLon: 71.41,
		DropoffLat:  51.14, DropoffLon: 71.44,
		StartLat:    51.11, StartLon: 71.41,
		FinishLat:   51.14, FinishLon: 71.44,
		Status:      StatusPending,
	}
	srB := &SubRide{
		RideID:      ride.RideID,
		PassengerID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		PickupLat:   51.12, PickupLon: 71.42,
		DropoffLat:  51.13, DropoffLon: 71.43,
		StartLat:    51.12, StartLon: 71.42,
		FinishLat:   51.13, FinishLon: 71.43,
		Status:      StatusPickedUp,
	}
	if err := srv.CreateSubRide(ctx, srA); err != nil {
		t.Fatalf("CreateSubRide A: %v", err)
	}
	if err := srv.CreateSubRide(ctx, srB); err != nil {
		t.Fatalf("CreateSubRide B: %v", err)
	}

	got, err := srv.GetSubRideByRideAndPassenger(ctx, ride.RideID, srA.PassengerID)
	if err != nil {
		t.Fatalf("GetSubRideByRideAndPassenger: %v", err)
	}
	if got.SubRideID != srA.SubRideID {
		t.Errorf("expected sub_ride_id %s, got %s", srA.SubRideID, got.SubRideID)
	}
	if got.Status != StatusPending {
		t.Errorf("expected status pending, got %s", got.Status)
	}

	gotB, err := srv.GetSubRideByRideAndPassenger(ctx, ride.RideID, srB.PassengerID)
	if err != nil {
		t.Fatalf("GetSubRideByRideAndPassenger B: %v", err)
	}
	if gotB.SubRideID != srB.SubRideID {
		t.Errorf("expected sub_ride_id %s, got %s", srB.SubRideID, gotB.SubRideID)
	}
}

func TestGetSubRideByRideAndPassenger_NotFound(t *testing.T) {
	dbOnce = sync.Once{}
	dbInstance = nil
	srv := New(testConnStr)
	ctx := context.Background()

	if _, err := srv.DeleteAllRides(ctx); err != nil {
		t.Fatalf("clean: %v", err)
	}

	ride := &Ride{
		DriverID:      "11111111-1111-1111-1111-111111111111",
		RouteID:       "22222222-2222-2222-2222-222222222222",
		GeometryWKT:   "LINESTRING(71.40 51.10, 71.45 51.15)",
		StartLat:      51.10, StartLon: 71.40,
		FinishLat:     51.15, FinishLon: 71.45,
		TotalDuration: 600,
		MaxPassengers: 3,
		Status:        StatusPending,
	}
	if err := srv.CreateRide(ctx, ride); err != nil {
		t.Fatalf("CreateRide: %v", err)
	}

	_, err := srv.GetSubRideByRideAndPassenger(ctx, ride.RideID, "cccccccc-cccc-cccc-cccc-cccccccccccc")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestCancelSubRideIfActive_FlipsPending(t *testing.T) {
	dbOnce = sync.Once{}
	dbInstance = nil
	srv := New(testConnStr)
	ctx := context.Background()

	if _, err := srv.DeleteAllRides(ctx); err != nil {
		t.Fatalf("clean: %v", err)
	}

	ride := &Ride{
		DriverID: "11111111-1111-1111-1111-111111111111", RouteID: "22222222-2222-2222-2222-222222222222",
		GeometryWKT: "LINESTRING(71.40 51.10, 71.45 51.15)",
		StartLat:    51.10, StartLon: 71.40, FinishLat: 51.15, FinishLon: 71.45,
		TotalDuration: 600, MaxPassengers: 3, Status: StatusPending,
	}
	if err := srv.CreateRide(ctx, ride); err != nil {
		t.Fatalf("CreateRide: %v", err)
	}
	sr := &SubRide{
		RideID: ride.RideID, PassengerID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		PickupLat: 51.11, PickupLon: 71.41, DropoffLat: 51.14, DropoffLon: 71.44,
		StartLat: 51.11, StartLon: 71.41, FinishLat: 51.14, FinishLon: 71.44,
		Status: StatusPending,
	}
	if err := srv.CreateSubRide(ctx, sr); err != nil {
		t.Fatalf("CreateSubRide: %v", err)
	}

	did, err := srv.CancelSubRideIfActive(ctx, sr.SubRideID)
	if err != nil {
		t.Fatalf("CancelSubRideIfActive: %v", err)
	}
	if !did {
		t.Fatal("expected didCancel=true for pending sub-ride")
	}

	got, err := srv.GetSubRide(ctx, sr.SubRideID)
	if err != nil {
		t.Fatalf("GetSubRide: %v", err)
	}
	if got.Status != StatusCancelled {
		t.Errorf("expected status cancelled, got %s", got.Status)
	}
}

func TestCancelSubRideIfActive_FlipsPickedUp(t *testing.T) {
	dbOnce = sync.Once{}
	dbInstance = nil
	srv := New(testConnStr)
	ctx := context.Background()

	if _, err := srv.DeleteAllRides(ctx); err != nil {
		t.Fatalf("clean: %v", err)
	}
	ride := &Ride{
		DriverID: "11111111-1111-1111-1111-111111111111", RouteID: "22222222-2222-2222-2222-222222222222",
		GeometryWKT: "LINESTRING(71.40 51.10, 71.45 51.15)",
		StartLat:    51.10, StartLon: 71.40, FinishLat: 51.15, FinishLon: 71.45,
		TotalDuration: 600, MaxPassengers: 3, Status: StatusActive,
	}
	if err := srv.CreateRide(ctx, ride); err != nil {
		t.Fatalf("CreateRide: %v", err)
	}
	sr := &SubRide{
		RideID: ride.RideID, PassengerID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		PickupLat: 51.11, PickupLon: 71.41, DropoffLat: 51.14, DropoffLon: 71.44,
		StartLat: 51.11, StartLon: 71.41, FinishLat: 51.14, FinishLon: 71.44,
		Status: StatusPickedUp,
	}
	if err := srv.CreateSubRide(ctx, sr); err != nil {
		t.Fatalf("CreateSubRide: %v", err)
	}

	did, err := srv.CancelSubRideIfActive(ctx, sr.SubRideID)
	if err != nil {
		t.Fatalf("CancelSubRideIfActive: %v", err)
	}
	if !did {
		t.Fatal("expected didCancel=true for picked_up sub-ride")
	}
	got, err := srv.GetSubRide(ctx, sr.SubRideID)
	if err != nil {
		t.Fatalf("GetSubRide: %v", err)
	}
	if got.Status != StatusCancelled {
		t.Errorf("expected status cancelled, got %s", got.Status)
	}
}

func TestCancelSubRideIfActive_NoOpOnTerminal(t *testing.T) {
	dbOnce = sync.Once{}
	dbInstance = nil
	srv := New(testConnStr)
	ctx := context.Background()

	if _, err := srv.DeleteAllRides(ctx); err != nil {
		t.Fatalf("clean: %v", err)
	}

	for _, terminal := range []string{StatusCompleted, StatusCancelled} {
		ride := &Ride{
			DriverID: "11111111-1111-1111-1111-111111111111", RouteID: "22222222-2222-2222-2222-222222222222",
			GeometryWKT: "LINESTRING(71.40 51.10, 71.45 51.15)",
			StartLat:    51.10, StartLon: 71.40, FinishLat: 51.15, FinishLon: 71.45,
			TotalDuration: 600, MaxPassengers: 3, Status: StatusPending,
		}
		if err := srv.CreateRide(ctx, ride); err != nil {
			t.Fatalf("CreateRide: %v", err)
		}
		sr := &SubRide{
			RideID: ride.RideID, PassengerID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
			PickupLat: 51.11, PickupLon: 71.41, DropoffLat: 51.14, DropoffLon: 71.44,
			StartLat: 51.11, StartLon: 71.41, FinishLat: 51.14, FinishLon: 71.44,
			Status: StatusPending,
		}
		if err := srv.CreateSubRide(ctx, sr); err != nil {
			t.Fatalf("CreateSubRide: %v", err)
		}
		if err := srv.UpdateSubRideStatus(ctx, sr.SubRideID, terminal); err != nil {
			t.Fatalf("UpdateSubRideStatus(%s): %v", terminal, err)
		}

		did, err := srv.CancelSubRideIfActive(ctx, sr.SubRideID)
		if err != nil {
			t.Fatalf("CancelSubRideIfActive: %v", err)
		}
		if did {
			t.Errorf("[start=%s] expected didCancel=false, got true", terminal)
		}
		got, err := srv.GetSubRide(ctx, sr.SubRideID)
		if err != nil {
			t.Fatalf("GetSubRide: %v", err)
		}
		if got.Status != terminal {
			t.Errorf("[start=%s] expected status unchanged (%s), got %s", terminal, terminal, got.Status)
		}

		if _, err := srv.DeleteAllRides(ctx); err != nil {
			t.Fatalf("clean between cases: %v", err)
		}
	}
}
