package server

import "time"

type Coord struct {
	Lat float64 `json:"lat" binding:"required,min=-90,max=90"`
	Lon float64 `json:"lon" binding:"required,min=-180,max=180"`
}

type CreateDriverPathRequest struct {
	DriverID      string `json:"driver_id" binding:"required,uuid"`
	Start         Coord  `json:"start" binding:"required"`
	Finish        Coord  `json:"finish" binding:"required"`
	MaxPassengers int    `json:"max_passengers"`
}

type CreateDriverPathResponse struct {
	RideID  string `json:"ride_id"`
	RouteID string `json:"route_id"`
	Status  string `json:"status"`
}

type MatchPassengerRequest struct {
	PassengerID string `json:"passenger_id" binding:"required,uuid"`
	Pickup      Coord  `json:"pickup" binding:"required"`
	Dropoff     Coord  `json:"dropoff" binding:"required"`
}

type MatchPassengerResponse struct {
	SubRideID    string    `json:"sub_ride_id"`
	RideID       string    `json:"ride_id"`
	DriverID     string    `json:"driver_id"`
	PickupPoint  Coord     `json:"pickup_point"`
	DropoffPoint Coord     `json:"dropoff_point"`
	PickupWalkM  float64   `json:"pickup_walk_m"`
	DropoffWalkM float64   `json:"dropoff_walk_m"`
	TotalWalkM   float64   `json:"total_walk_m"`
	WalkToPickup *WalkPath `json:"walk_to_pickup,omitempty"`
}

type WalkPath struct {
	Geometry     []WalkCoord `json:"geometry"`
	WalkDistance string      `json:"walk_distance"`
	WalkDuration string      `json:"walk_duration"`
}

type WalkCoord struct {
	Lon float64 `json:"lon"`
	Lat float64 `json:"lat"`
}

type SetDriverLocationRequest struct {
	DriverID string  `json:"driver_id" binding:"required,uuid"`
	Lat      float64 `json:"lat" binding:"required,min=-90,max=90"`
	Lon      float64 `json:"lon" binding:"required,min=-180,max=180"`
}

type DriverHistoryItem struct {
	RideID         string     `json:"ride_id"`
	RouteID        string     `json:"route_id"`
	Status         string     `json:"status"`
	MaxPassengers  int        `json:"max_passengers"`
	StartLat       float64    `json:"start_lat"`
	StartLon       float64    `json:"start_lon"`
	FinishLat      float64    `json:"finish_lat"`
	FinishLon      float64    `json:"finish_lon"`
	TotalDuration  int        `json:"total_duration"`
	PassengerCount int        `json:"passenger_count"`
	TotalEarnings  int        `json:"total_earnings"`
	StartedAt      *time.Time `json:"started_at"`
	CompletedAt    *time.Time `json:"completed_at"`
	CreatedAt      time.Time  `json:"created_at"`
}

type PassengerHistoryItem struct {
	SubRideID   string     `json:"sub_ride_id"`
	RideID      string     `json:"ride_id"`
	DriverID    string     `json:"driver_id"`
	Status      string     `json:"status"`
	Price       *int       `json:"price"`
	PickupLat   float64    `json:"pickup_lat"`
	PickupLon   float64    `json:"pickup_lon"`
	DropoffLat  float64    `json:"dropoff_lat"`
	DropoffLon  float64    `json:"dropoff_lon"`
	CompletedAt *time.Time `json:"completed_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

type HistoryResponse struct {
	Rides any `json:"rides"`
	Total int `json:"total"`
	Page  int `json:"page"`
	Limit int `json:"limit"`
}
