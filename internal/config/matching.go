package config

import (
	"context"
	"log"
	"sync/atomic"
	"time"
)

// MatchingParams holds tunable matching algorithm parameters.
type MatchingParams struct {
	PickupRadiusM      float64 `json:"pickupRadiusM"`
	DropoffRadiusM     float64 `json:"dropoffRadiusM"`
	MaxPassengers      int     `json:"maxPassengers"`
	MinETASeconds      int     `json:"minEtaSeconds"`
	MinDriverDistanceM float64 `json:"minDriverDistanceM"`
}

// DefaultMatchingParams returns defaults from env vars (already loaded by Config).
func DefaultMatchingParams() *MatchingParams {
	return &MatchingParams{
		PickupRadiusM:      envFloatOrDefault("PICKUP_RADIUS_M", 500),
		DropoffRadiusM:     envFloatOrDefault("DROPOFF_RADIUS_M", 500),
		MaxPassengers:      envIntOrDefault("MAX_PASSENGERS", 3),
		MinETASeconds:      envIntOrDefault("MIN_ETA_SECONDS", 180),
		MinDriverDistanceM: envFloatOrDefault("MIN_DRIVER_DISTANCE_M", 2000),
	}
}

// LoadFunc loads matching params from a data source (e.g. database).
// Returns nil if no stored params exist (use defaults).
type LoadFunc func(ctx context.Context) (*MatchingParams, error)

// MatchingParamsLoader provides lock-free access to matching params.
type MatchingParamsLoader struct {
	params atomic.Pointer[MatchingParams]
}

// NewMatchingParamsLoader creates a loader with initial params.
func NewMatchingParamsLoader(initial *MatchingParams) *MatchingParamsLoader {
	l := &MatchingParamsLoader{}
	l.params.Store(initial)
	return l
}

// Get returns the current matching params. Lock-free.
func (l *MatchingParamsLoader) Get() *MatchingParams {
	return l.params.Load()
}

// Store atomically swaps to new params.
func (l *MatchingParamsLoader) Store(p *MatchingParams) {
	l.params.Store(p)
}

// StartPeriodicReload launches a background goroutine that reloads params
// from the given load function every interval. Stops when ctx is cancelled.
func (l *MatchingParamsLoader) StartPeriodicReload(ctx context.Context, interval time.Duration, load LoadFunc) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				p, err := load(ctx)
				if err != nil {
					log.Printf("matching params reload failed, keeping previous: %v", err)
					continue
				}
				if p != nil {
					l.params.Store(p)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}
