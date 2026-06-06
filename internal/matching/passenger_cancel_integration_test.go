//go:build integration

package matching

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"rides/internal/database"
)

var integrationConnStr string

func TestMain(m *testing.M) {
	ctx := context.Background()

	dbContainer, err := postgres.Run(
		ctx,
		"postgis/postgis:17-3.5",
		postgres.WithDatabase("rides"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		log.Fatalf("postgres container: %v", err)
	}

	host, err := dbContainer.Host(ctx)
	if err != nil {
		log.Fatalf("host: %v", err)
	}
	port, err := dbContainer.MappedPort(ctx, "5432/tcp")
	if err != nil {
		log.Fatalf("port: %v", err)
	}
	integrationConnStr = fmt.Sprintf("postgres://test:test@%s:%s/rides?sslmode=disable", host, port.Port())

	if err := database.RunMigrations(integrationConnStr, "../../migrations"); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	code := m.Run()

	if err := dbContainer.Terminate(ctx); err != nil {
		log.Printf("teardown: %v", err)
	}
	os.Exit(code)
}

// freshDB reopens the singleton against the integration container — each
// test resets `dbOnce` so it can take a fresh instance.
func freshDB(t *testing.T) database.Service {
	t.Helper()
	// We can't reset the sync.Once from outside the database package; instead
	// we use the same singleton across tests. Tests must clean up via
	// DeleteAllRides at the top of each test.
	return database.New(integrationConnStr)
}

func seedRideAndSubRide(t *testing.T, db database.Service, status string) (rideID, subRideID, passengerID, driverID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.DeleteAllRides(ctx); err != nil {
		t.Fatalf("clean: %v", err)
	}

	driverID = "11111111-1111-1111-1111-111111111111"
	passengerID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"

	ride := &database.Ride{
		DriverID:      driverID,
		RouteID:       "22222222-2222-2222-2222-222222222222",
		GeometryWKT:   "LINESTRING(71.40 51.10, 71.45 51.15)",
		StartLat:      51.10, StartLon: 71.40,
		FinishLat:     51.15, FinishLon: 71.45,
		TotalDuration: 600,
		MaxPassengers: 3,
		Status:        database.StatusPending,
	}
	if err := db.CreateRide(ctx, ride); err != nil {
		t.Fatalf("CreateRide: %v", err)
	}
	rideID = ride.RideID

	sr := &database.SubRide{
		RideID:      rideID,
		PassengerID: passengerID,
		PickupLat:   51.11, PickupLon: 71.41,
		DropoffLat:  51.14, DropoffLon: 71.44,
		StartLat:    51.11, StartLon: 71.41,
		FinishLat:   51.14, FinishLon: 71.44,
		Status:      status,
	}
	if err := db.CreateSubRide(ctx, sr); err != nil {
		t.Fatalf("CreateSubRide: %v", err)
	}
	subRideID = sr.SubRideID
	return
}

func TestIntegration_PassengerCancel_HappyPath(t *testing.T) {
	db := freshDB(t)
	rideID, subRideID, passengerID, driverID := seedRideAndSubRide(t, db, database.StatusPending)

	sender := &mockSender{}
	h := &Handler{db: db, producer: sender}

	h.handleCancel(context.Background(), InboundMessage{
		Action: ActionCancel, RideID: rideID, UserID: passengerID, Reason: "integration test",
	})

	// DB assertions
	got, err := db.GetSubRide(context.Background(), subRideID)
	if err != nil {
		t.Fatalf("GetSubRide: %v", err)
	}
	if got.Status != database.StatusCancelled {
		t.Errorf("expected sub_ride status cancelled, got %s", got.Status)
	}
	ride, err := db.GetRide(context.Background(), rideID)
	if err != nil {
		t.Fatalf("GetRide: %v", err)
	}
	if ride.Status != database.StatusPending {
		t.Errorf("parent ride must be untouched, got %s", ride.Status)
	}

	// Event assertions
	if firstSubRideCancelledFor(sender, driverID) == nil {
		t.Error("expected driver-bound sub_ride_cancelled")
	}
	if firstSubRideCancelledFor(sender, passengerID) == nil {
		t.Error("expected passenger-bound sub_ride_cancelled")
	}
}

func TestIntegration_PassengerCancel_IdempotentOnTerminal(t *testing.T) {
	db := freshDB(t)
	rideID, subRideID, passengerID, _ := seedRideAndSubRide(t, db, database.StatusPending)

	// Pre-cancel the sub-ride out of band.
	if err := db.UpdateSubRideStatus(context.Background(), subRideID, database.StatusCancelled); err != nil {
		t.Fatalf("UpdateSubRideStatus: %v", err)
	}

	sender := &mockSender{}
	h := &Handler{db: db, producer: sender}

	h.handleCancel(context.Background(), InboundMessage{
		Action: ActionCancel, RideID: rideID, UserID: passengerID,
	})

	// Sub-ride must still be cancelled, but no success events were fired.
	for _, m := range sender.messages() {
		if _, ok := m.value.(SubRideCancelledMessage); ok {
			t.Errorf("expected no SubRideCancelledMessage on idempotent path, got %+v", m)
		}
	}
	errEvt := firstNotificationByEvent(sender, "ride_error")
	if errEvt == nil {
		t.Fatal("expected ride_error notification")
	}
	if errEvt.Message != "your sub-ride is already cancelled" {
		t.Errorf("unexpected error: %s", errEvt.Message)
	}
}
