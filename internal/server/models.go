package server

type Coord struct {
	Lat float64 `json:"lat" binding:"required,min=-90,max=90"`
	Lon float64 `json:"lon" binding:"required,min=-180,max=180"`
}

type CreateDriverPathRequest struct {
	DriverID string `json:"driver_id" binding:"required,uuid"`
	Start    Coord  `json:"start" binding:"required"`
	Finish   Coord  `json:"finish" binding:"required"`
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
	DriverID string  `json:"driver_id" binding:"required"`
	Lat      float64 `json:"lat" binding:"required"`
	Lon      float64 `json:"lon" binding:"required"`
}
