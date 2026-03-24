package config

import (
	"testing"
)

func TestMatchingParamsDefaults(t *testing.T) {
	p := DefaultMatchingParams()
	if p.PickupRadiusM != 500 {
		t.Errorf("got PickupRadiusM %f, want 500", p.PickupRadiusM)
	}
	if p.MaxPassengers != 3 {
		t.Errorf("got MaxPassengers %d, want 3", p.MaxPassengers)
	}
	if p.MinETASeconds != 180 {
		t.Errorf("got MinETASeconds %d, want 180", p.MinETASeconds)
	}
}

func TestMatchingParamsLoader_StoreLoad(t *testing.T) {
	loader := NewMatchingParamsLoader(DefaultMatchingParams())
	p := loader.Get()
	if p.PickupRadiusM != 500 {
		t.Errorf("got PickupRadiusM %f, want 500", p.PickupRadiusM)
	}

	updated := &MatchingParams{
		PickupRadiusM:      600,
		DropoffRadiusM:     600,
		MaxPassengers:      4,
		MinETASeconds:      120,
		MinDriverDistanceM: 1500,
	}
	loader.Store(updated)

	p = loader.Get()
	if p.PickupRadiusM != 600 {
		t.Errorf("got PickupRadiusM %f, want 600", p.PickupRadiusM)
	}
	if p.MaxPassengers != 4 {
		t.Errorf("got MaxPassengers %d, want 4", p.MaxPassengers)
	}
}
