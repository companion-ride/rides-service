package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"rides/internal/config"
	"rides/internal/database"
	"rides/internal/location"
	"rides/internal/pathfinder"
)

// pathfinderClient is the subset of pathfinder.Client the server depends on.
// Defined here so handlers can be unit-tested with a fake.
type pathfinderClient interface {
	CreateRoute(ctx context.Context, authHeader string, startLat, startLon, finishLat, finishLon float64) (*pathfinder.RouteResponse, error)
	CreateCustomRoute(ctx context.Context, authHeader string, segments []pathfinder.SegmentInput) (*pathfinder.RouteResponse, error)
	GetRoute(ctx context.Context, authHeader string, routeID string) (*pathfinder.RouteResponse, error)
	WalkToClosest(ctx context.Context, authHeader string, routeID string, lat, lon float64) (*pathfinder.WalkResponse, error)
}

type Server struct {
	port           int
	cfg            *config.Config
	db             database.Service
	pathfinder     pathfinderClient
	location       *location.Client
	matchingParams *config.MatchingParamsLoader
}

func NewServer(cfg *config.Config, db database.Service, mp *config.MatchingParamsLoader) *http.Server {
	s := &Server{
		port:           cfg.Port,
		cfg:            cfg,
		db:             db,
		pathfinder:     pathfinder.NewClient(cfg.PathfinderURL),
		location:       location.NewClient(cfg.LocationServiceURL, cfg.InternalAPIKey),
		matchingParams: mp,
	}

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      s.RegisterRoutes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	return server
}
