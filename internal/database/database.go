package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Ride struct {
	RideID        string
	DriverID      string
	RouteID       string
	GeometryWKT   string
	StartLat      float64
	StartLon      float64
	FinishLat     float64
	FinishLon     float64
	TotalDuration int
	Status        string
	StartedAt     *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type SubRide struct {
	SubRideID   string
	RideID      string
	PassengerID string
	PickupLat   float64
	PickupLon   float64
	DropoffLat  float64
	DropoffLon  float64
	StartLat    float64
	StartLon    float64
	FinishLat   float64
	FinishLon   float64
	Status      string
}

type MatchParams struct {
	PickupLon    float64
	PickupLat    float64
	DropoffLon   float64
	DropoffLat   float64
	PickupRadius float64
	DropoffRadius float64
	MaxResults   int
}

type MatchResult struct {
	RideID        string
	DriverID      string
	RouteID       string
	TotalDuration int
	Status        string
	PickupWalkM   float64
	DropoffWalkM  float64
	PickupFrac    float64
	DropoffFrac   float64
	PickupPointLat  float64
	PickupPointLon  float64
	DropoffPointLat float64
	DropoffPointLon float64
}

type AdminListParams struct {
	Status string
	Search string
	Limit  int
	Offset int
}

type AdminRideRow struct {
	RideID        string
	DriverID      string
	RouteID       string
	FromAddress   *string
	ToAddress     *string
	Status        string
	Price         *int
	DistanceM     *int
	TotalDuration int
	PaymentMethod *string
	Passengers    int
	StartLat      float64
	StartLon      float64
	FinishLat     float64
	FinishLon     float64
	StartedAt     *time.Time
	CompletedAt   *time.Time
	CancelledAt   *time.Time
	CreatedAt     time.Time
}

type AdminSubRideRow struct {
	SubRideID   string
	PassengerID string
	PickupLat   float64
	PickupLon   float64
	DropoffLat  float64
	DropoffLon  float64
	Status      string
	CompletedAt *time.Time
	CancelledAt *time.Time
	CreatedAt   time.Time
}

type MatchingStats struct {
	TotalRides     int
	RidesByStatus  map[string]int
	MatchedRides   int
	TotalSubRides  int
	AvgWaitSeconds float64
	CancelledRides int
}

type Service interface {
	Health() map[string]string
	Close() error

	CreateRide(ctx context.Context, ride *Ride) error
	GetRide(ctx context.Context, rideID string) (*Ride, error)
	UpdateRideStatus(ctx context.Context, rideID, status string) error

	CreateSubRide(ctx context.Context, subRide *SubRide) error
	CountActiveSubRides(ctx context.Context, rideID string) (int, error)

	FindMatchingRides(ctx context.Context, params MatchParams) ([]MatchResult, error)
	GetDriverFraction(ctx context.Context, rideID string, lon, lat float64) (float64, error)
	ListActiveRides(ctx context.Context) ([]Ride, error)

	// Admin queries
	GetAdminSettings(ctx context.Context) (json.RawMessage, error)
	UpsertAdminSettings(ctx context.Context, settings json.RawMessage) error
	GetMatchingParams(ctx context.Context) (json.RawMessage, error)
	UpsertMatchingParams(ctx context.Context, params json.RawMessage) error
	ListAdminRides(ctx context.Context, params AdminListParams) ([]AdminRideRow, int, error)
	GetAdminRideDetail(ctx context.Context, rideID string) (*AdminRideRow, []AdminSubRideRow, error)
	GetMatchingStats(ctx context.Context) (*MatchingStats, error)
}

type service struct {
	db *sql.DB
}

var dbInstance *service

func New(connStr string) Service {
	if dbInstance != nil {
		return dbInstance
	}
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		log.Fatal(err)
	}
	dbInstance = &service{db: db}
	return dbInstance
}

func (s *service) Health() map[string]string {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	stats := make(map[string]string)

	err := s.db.PingContext(ctx)
	if err != nil {
		stats["status"] = "down"
		stats["error"] = fmt.Sprintf("db down: %v", err)
		log.Fatalf("db down: %v", err)
		return stats
	}

	stats["status"] = "up"
	stats["message"] = "It's healthy"

	dbStats := s.db.Stats()
	stats["open_connections"] = strconv.Itoa(dbStats.OpenConnections)
	stats["in_use"] = strconv.Itoa(dbStats.InUse)
	stats["idle"] = strconv.Itoa(dbStats.Idle)
	stats["wait_count"] = strconv.FormatInt(dbStats.WaitCount, 10)
	stats["wait_duration"] = dbStats.WaitDuration.String()
	stats["max_idle_closed"] = strconv.FormatInt(dbStats.MaxIdleClosed, 10)
	stats["max_lifetime_closed"] = strconv.FormatInt(dbStats.MaxLifetimeClosed, 10)

	if dbStats.OpenConnections > 40 {
		stats["message"] = "The database is experiencing heavy load."
	}
	if dbStats.WaitCount > 1000 {
		stats["message"] = "The database has a high number of wait events, indicating potential bottlenecks."
	}
	if dbStats.MaxIdleClosed > int64(dbStats.OpenConnections)/2 {
		stats["message"] = "Many idle connections are being closed, consider revising the connection pool settings."
	}
	if dbStats.MaxLifetimeClosed > int64(dbStats.OpenConnections)/2 {
		stats["message"] = "Many connections are being closed due to max lifetime, consider increasing max lifetime or revising the connection usage pattern."
	}

	return stats
}

func (s *service) Close() error {
	log.Println("Disconnected from database")
	return s.db.Close()
}

// EnsureSchema creates the given schema if it does not already exist.
// Must be called before RunMigrations when using a non-public search_path.
func EnsureSchema(connStr, schema string) error {
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		return fmt.Errorf("opening db for schema creation: %w", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE SCHEMA IF NOT EXISTS "` + schema + `"`); err != nil {
		return fmt.Errorf("creating schema %q: %w", schema, err)
	}
	return nil
}

func (s *service) CreateRide(ctx context.Context, ride *Ride) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO rides (driver_id, route_id, geometry, start_lat, start_lon, finish_lat, finish_lon, total_duration, status)
		 VALUES ($1, $2, ST_GeomFromText($3, 4326), $4, $5, $6, $7, $8, $9)
		 RETURNING ride_id`,
		ride.DriverID, ride.RouteID, ride.GeometryWKT,
		ride.StartLat, ride.StartLon, ride.FinishLat, ride.FinishLon,
		ride.TotalDuration, ride.Status,
	).Scan(&ride.RideID)
	if err != nil {
		return fmt.Errorf("inserting ride: %w", err)
	}
	return nil
}

func (s *service) GetRide(ctx context.Context, rideID string) (*Ride, error) {
	var ride Ride
	err := s.db.QueryRowContext(ctx,
		`SELECT ride_id, driver_id, route_id, ST_AsText(geometry),
		        start_lat, start_lon, finish_lat, finish_lon,
		        total_duration, status, started_at, created_at, updated_at
		 FROM rides WHERE ride_id = $1`,
		rideID,
	).Scan(&ride.RideID, &ride.DriverID, &ride.RouteID, &ride.GeometryWKT,
		&ride.StartLat, &ride.StartLon, &ride.FinishLat, &ride.FinishLon,
		&ride.TotalDuration, &ride.Status, &ride.StartedAt, &ride.CreatedAt, &ride.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &ride, nil
}

func (s *service) UpdateRideStatus(ctx context.Context, rideID, status string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE rides SET status = $1, updated_at = now() WHERE ride_id = $2`,
		status, rideID,
	)
	return err
}

func (s *service) CreateSubRide(ctx context.Context, sr *SubRide) error {
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO sub_rides (ride_id, passenger_id, pickup_lat, pickup_lon, dropoff_lat, dropoff_lon,
		                        start_lat, start_lon, finish_lat, finish_lon, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 RETURNING sub_ride_id`,
		sr.RideID, sr.PassengerID,
		sr.PickupLat, sr.PickupLon, sr.DropoffLat, sr.DropoffLon,
		sr.StartLat, sr.StartLon, sr.FinishLat, sr.FinishLon,
		sr.Status,
	).Scan(&sr.SubRideID)
	if err != nil {
		return fmt.Errorf("inserting sub_ride: %w", err)
	}
	return nil
}

func (s *service) CountActiveSubRides(ctx context.Context, rideID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sub_rides WHERE ride_id = $1 AND status IN ('pending', 'active')`,
		rideID,
	).Scan(&count)
	return count, err
}

func (s *service) FindMatchingRides(ctx context.Context, params MatchParams) ([]MatchResult, error) {
	query := `
WITH candidates AS (
    SELECT r.ride_id, r.driver_id, r.route_id, r.geometry, r.total_duration, r.status
    FROM rides r
    WHERE r.status IN ('pending', 'active')
      AND ST_DWithin(r.geometry::geography, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography, $5)
      AND ST_DWithin(r.geometry::geography, ST_SetSRID(ST_MakePoint($3, $4), 4326)::geography, $6)
)
SELECT
    c.ride_id, c.driver_id, c.route_id, c.total_duration, c.status,
    ST_Distance(c.geometry::geography, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography) AS pickup_walk_m,
    ST_Distance(c.geometry::geography, ST_SetSRID(ST_MakePoint($3, $4), 4326)::geography) AS dropoff_walk_m,
    ST_LineLocatePoint(c.geometry, ST_SetSRID(ST_MakePoint($1, $2), 4326)) AS pickup_frac,
    ST_LineLocatePoint(c.geometry, ST_SetSRID(ST_MakePoint($3, $4), 4326)) AS dropoff_frac,
    ST_Y(ST_ClosestPoint(c.geometry, ST_SetSRID(ST_MakePoint($1, $2), 4326))) AS pickup_point_lat,
    ST_X(ST_ClosestPoint(c.geometry, ST_SetSRID(ST_MakePoint($1, $2), 4326))) AS pickup_point_lon,
    ST_Y(ST_ClosestPoint(c.geometry, ST_SetSRID(ST_MakePoint($3, $4), 4326))) AS dropoff_point_lat,
    ST_X(ST_ClosestPoint(c.geometry, ST_SetSRID(ST_MakePoint($3, $4), 4326))) AS dropoff_point_lon
FROM candidates c
WHERE ST_LineLocatePoint(c.geometry, ST_SetSRID(ST_MakePoint($1, $2), 4326))
    < ST_LineLocatePoint(c.geometry, ST_SetSRID(ST_MakePoint($3, $4), 4326))
ORDER BY (
    ST_Distance(c.geometry::geography, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography) +
    ST_Distance(c.geometry::geography, ST_SetSRID(ST_MakePoint($3, $4), 4326)::geography)
)
LIMIT $7`

	rows, err := s.db.QueryContext(ctx, query,
		params.PickupLon, params.PickupLat,
		params.DropoffLon, params.DropoffLat,
		params.PickupRadius, params.DropoffRadius,
		params.MaxResults,
	)
	if err != nil {
		return nil, fmt.Errorf("finding matching rides: %w", err)
	}
	defer rows.Close()

	var results []MatchResult
	for rows.Next() {
		var m MatchResult
		if err := rows.Scan(
			&m.RideID, &m.DriverID, &m.RouteID, &m.TotalDuration, &m.Status,
			&m.PickupWalkM, &m.DropoffWalkM,
			&m.PickupFrac, &m.DropoffFrac,
			&m.PickupPointLat, &m.PickupPointLon,
			&m.DropoffPointLat, &m.DropoffPointLon,
		); err != nil {
			return nil, fmt.Errorf("scanning match result: %w", err)
		}
		results = append(results, m)
	}
	return results, rows.Err()
}

func (s *service) GetDriverFraction(ctx context.Context, rideID string, lon, lat float64) (float64, error) {
	var frac float64
	err := s.db.QueryRowContext(ctx,
		`SELECT ST_LineLocatePoint(geometry, ST_SetSRID(ST_MakePoint($2, $3), 4326))
		 FROM rides WHERE ride_id = $1`,
		rideID, lon, lat,
	).Scan(&frac)
	if err != nil {
		return 0, fmt.Errorf("getting driver fraction: %w", err)
	}
	return frac, nil
}

func (s *service) ListActiveRides(ctx context.Context) ([]Ride, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT ride_id, driver_id, route_id, start_lat, start_lon, finish_lat, finish_lon, status
		 FROM rides WHERE status IN ('pending', 'active') ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rides []Ride
	for rows.Next() {
		var r Ride
		if err := rows.Scan(&r.RideID, &r.DriverID, &r.RouteID,
			&r.StartLat, &r.StartLon, &r.FinishLat, &r.FinishLon, &r.Status); err != nil {
			return nil, err
		}
		rides = append(rides, r)
	}
	return rides, rows.Err()
}

func (s *service) GetAdminSettings(ctx context.Context) (json.RawMessage, error) {
	var settings json.RawMessage
	err := s.db.QueryRowContext(ctx,
		`SELECT settings FROM admin_settings WHERE id = 1`,
	).Scan(&settings)
	if err == sql.ErrNoRows {
		return json.RawMessage(`{}`), nil
	}
	return settings, err
}

func (s *service) UpsertAdminSettings(ctx context.Context, settings json.RawMessage) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO admin_settings (id, settings, updated_at)
		 VALUES (1, $1, NOW())
		 ON CONFLICT (id) DO UPDATE SET settings = $1, updated_at = NOW()`,
		settings,
	)
	return err
}

func (s *service) GetMatchingParams(ctx context.Context) (json.RawMessage, error) {
	var params json.RawMessage
	err := s.db.QueryRowContext(ctx,
		`SELECT matching_params FROM admin_settings WHERE id = 1`,
	).Scan(&params)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return params, err
}

func (s *service) UpsertMatchingParams(ctx context.Context, params json.RawMessage) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO admin_settings (id, matching_params, updated_at)
		 VALUES (1, $1, NOW())
		 ON CONFLICT (id) DO UPDATE SET matching_params = $1, updated_at = NOW()`,
		params,
	)
	return err
}

func (s *service) ListAdminRides(ctx context.Context, params AdminListParams) ([]AdminRideRow, int, error) {
	countQuery := `SELECT COUNT(*) FROM rides WHERE ($1 = '' OR status = $1)
		AND ($2 = '' OR ride_id::text LIKE $2 || '%' OR driver_id::text LIKE $2 || '%')`
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, params.Status, params.Search).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("counting admin rides: %w", err)
	}

	query := `SELECT r.ride_id, r.driver_id, r.route_id, r.from_address, r.to_address,
		r.status, r.price, r.distance_m, r.total_duration, r.payment_method,
		COALESCE(sr.cnt, 0), r.created_at
		FROM rides r
		LEFT JOIN (
			SELECT ride_id, COUNT(*) AS cnt
			FROM sub_rides WHERE status IN ('pending', 'active', 'completed')
			GROUP BY ride_id
		) sr ON sr.ride_id = r.ride_id
		WHERE ($1 = '' OR r.status = $1)
		  AND ($2 = '' OR r.ride_id::text LIKE $2 || '%' OR r.driver_id::text LIKE $2 || '%')
		ORDER BY r.created_at DESC
		LIMIT $3 OFFSET $4`

	rows, err := s.db.QueryContext(ctx, query, params.Status, params.Search, params.Limit, params.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("listing admin rides: %w", err)
	}
	defer rows.Close()

	var rides []AdminRideRow
	for rows.Next() {
		var r AdminRideRow
		if err := rows.Scan(&r.RideID, &r.DriverID, &r.RouteID, &r.FromAddress, &r.ToAddress,
			&r.Status, &r.Price, &r.DistanceM, &r.TotalDuration, &r.PaymentMethod,
			&r.Passengers, &r.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scanning admin ride: %w", err)
		}
		rides = append(rides, r)
	}
	return rides, total, rows.Err()
}

func (s *service) GetAdminRideDetail(ctx context.Context, rideID string) (*AdminRideRow, []AdminSubRideRow, error) {
	var r AdminRideRow
	err := s.db.QueryRowContext(ctx,
		`SELECT ride_id, driver_id, route_id, from_address, to_address,
		        status, price, distance_m, total_duration, payment_method,
		        start_lat, start_lon, finish_lat, finish_lon,
		        started_at, completed_at, cancelled_at, created_at
		 FROM rides WHERE ride_id = $1`,
		rideID,
	).Scan(&r.RideID, &r.DriverID, &r.RouteID, &r.FromAddress, &r.ToAddress,
		&r.Status, &r.Price, &r.DistanceM, &r.TotalDuration, &r.PaymentMethod,
		&r.StartLat, &r.StartLon, &r.FinishLat, &r.FinishLon,
		&r.StartedAt, &r.CompletedAt, &r.CancelledAt, &r.CreatedAt)
	if err != nil {
		return nil, nil, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT sub_ride_id, passenger_id, pickup_lat, pickup_lon, dropoff_lat, dropoff_lon,
		        status, completed_at, cancelled_at, created_at
		 FROM sub_rides WHERE ride_id = $1 ORDER BY created_at`,
		rideID,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("listing sub_rides: %w", err)
	}
	defer rows.Close()

	var subs []AdminSubRideRow
	for rows.Next() {
		var sr AdminSubRideRow
		if err := rows.Scan(&sr.SubRideID, &sr.PassengerID,
			&sr.PickupLat, &sr.PickupLon, &sr.DropoffLat, &sr.DropoffLon,
			&sr.Status, &sr.CompletedAt, &sr.CancelledAt, &sr.CreatedAt); err != nil {
			return nil, nil, fmt.Errorf("scanning sub_ride: %w", err)
		}
		subs = append(subs, sr)
	}
	return &r, subs, rows.Err()
}

func (s *service) GetMatchingStats(ctx context.Context) (*MatchingStats, error) {
	stats := &MatchingStats{
		RidesByStatus: make(map[string]int),
	}

	rows, err := s.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM rides GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("querying ride status counts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		stats.RidesByStatus[status] = count
		stats.TotalRides += count
		if status == "cancelled" {
			stats.CancelledRides = count
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT ride_id), COUNT(*) FROM sub_rides`,
	).Scan(&stats.MatchedRides, &stats.TotalSubRides)
	if err != nil {
		return nil, fmt.Errorf("querying sub_ride stats: %w", err)
	}

	var avgWait *float64
	err = s.db.QueryRowContext(ctx,
		`SELECT EXTRACT(EPOCH FROM AVG(started_at - created_at))
		 FROM rides WHERE started_at IS NOT NULL`,
	).Scan(&avgWait)
	if err != nil {
		return nil, fmt.Errorf("querying avg wait: %w", err)
	}
	if avgWait != nil {
		stats.AvgWaitSeconds = *avgWait
	}

	return stats, nil
}
