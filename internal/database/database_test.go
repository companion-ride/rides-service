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
