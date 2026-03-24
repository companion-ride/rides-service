package server

import (
	"encoding/json"
	"time"
)

// AdminRideListItem is a single ride in the admin listing.
type AdminRideListItem struct {
	ID            string    `json:"id"`
	DriverID      string    `json:"driverId"`
	FromAddress   *string   `json:"fromAddress"`
	ToAddress     *string   `json:"toAddress"`
	Status        string    `json:"status"`
	Price         *int      `json:"price"`
	DistanceM     *int      `json:"distanceM"`
	TotalDuration int       `json:"totalDuration"`
	PaymentMethod *string   `json:"paymentMethod"`
	Passengers    int       `json:"passengers"`
	CreatedAt     time.Time `json:"createdAt"`
}

// AdminRideListResponse is the paginated response for GET /admin/rides.
type AdminRideListResponse struct {
	Rides []AdminRideListItem `json:"rides"`
	Total int                 `json:"total"`
	Page  int                 `json:"page"`
	Limit int                 `json:"limit"`
}

// AdminSubRideItem is a sub_ride in the ride detail response.
type AdminSubRideItem struct {
	SubRideID   string     `json:"subRideId"`
	PassengerID string     `json:"passengerId"`
	PickupLat   float64    `json:"pickupLat"`
	PickupLon   float64    `json:"pickupLon"`
	DropoffLat  float64    `json:"dropoffLat"`
	DropoffLon  float64    `json:"dropoffLon"`
	Status      string     `json:"status"`
	CompletedAt *time.Time `json:"completedAt"`
	CancelledAt *time.Time `json:"cancelledAt"`
	CreatedAt   time.Time  `json:"createdAt"`
}

// AdminRideDetailResponse is the response for GET /admin/rides/:id.
type AdminRideDetailResponse struct {
	ID            string             `json:"id"`
	DriverID      string             `json:"driverId"`
	RouteID       string             `json:"routeId"`
	FromAddress   *string            `json:"fromAddress"`
	ToAddress     *string            `json:"toAddress"`
	Status        string             `json:"status"`
	Price         *int               `json:"price"`
	DistanceM     *int               `json:"distanceM"`
	TotalDuration int                `json:"totalDuration"`
	PaymentMethod *string            `json:"paymentMethod"`
	StartLat      float64            `json:"startLat"`
	StartLon      float64            `json:"startLon"`
	FinishLat     float64            `json:"finishLat"`
	FinishLon     float64            `json:"finishLon"`
	StartedAt     *time.Time         `json:"startedAt"`
	CompletedAt   *time.Time         `json:"completedAt"`
	CancelledAt   *time.Time         `json:"cancelledAt"`
	CreatedAt     time.Time          `json:"createdAt"`
	SubRides      []AdminSubRideItem `json:"subRides"`
}

// MatchingStatsResponse is the response for GET /admin/matching/stats.
type MatchingStatsResponse struct {
	TotalRides           int            `json:"totalRides"`
	RidesByStatus        map[string]int `json:"ridesByStatus"`
	MatchRate            float64        `json:"matchRate"`
	AvgWaitSeconds       float64        `json:"avgWaitSeconds"`
	TotalSubRides        int            `json:"totalSubRides"`
	AvgPassengersPerRide float64        `json:"avgPassengersPerRide"`
	CancellationRate     float64        `json:"cancellationRate"`
}

// MatchingParamsRequest is the request body for PUT /admin/matching/params.
type MatchingParamsRequest struct {
	PickupRadiusM      int `json:"pickupRadiusM" binding:"required,min=100,max=10000"`
	DropoffRadiusM     int `json:"dropoffRadiusM" binding:"required,min=100,max=10000"`
	MaxPassengers      int `json:"maxPassengers" binding:"required,min=1,max=10"`
	MinEtaSeconds      int `json:"minEtaSeconds" binding:"required,min=30,max=600"`
	MinDriverDistanceM int `json:"minDriverDistanceM" binding:"required,min=100,max=10000"`
}

// MatchingParamsResponse is the response for GET /admin/matching/params.
type MatchingParamsResponse struct {
	PickupRadiusM      float64 `json:"pickupRadiusM"`
	DropoffRadiusM     float64 `json:"dropoffRadiusM"`
	MaxPassengers      int     `json:"maxPassengers"`
	MinEtaSeconds      int     `json:"minEtaSeconds"`
	MinDriverDistanceM float64 `json:"minDriverDistanceM"`
}

// AdminSettingsPayload is opaque JSON for GET/PUT /admin/settings.
type AdminSettingsPayload = json.RawMessage
