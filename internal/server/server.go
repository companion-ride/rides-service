package server

import (
	"fmt"
	"net/http"
	"time"

	"rides/internal/config"
	"rides/internal/database"
	"rides/internal/location"
	"rides/internal/pathfinder"
)

type Server struct {
	port           int
	cfg            *config.Config
	db             database.Service
	pathfinder     *pathfinder.Client
	location       *location.Client
	matchingParams *config.MatchingParamsLoader
}

func NewServer(cfg *config.Config, db database.Service, mp *config.MatchingParamsLoader) *http.Server {
	s := &Server{
		port:           cfg.Port,
		cfg:            cfg,
		db:             db,
		pathfinder:     pathfinder.NewClient(cfg.PathfinderURL),
		location:       location.NewClient(cfg.LocationServiceURL),
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
