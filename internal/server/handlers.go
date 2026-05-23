package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"rides/internal/auth"
	"rides/internal/database"
	"rides/internal/pathfinder"
)

func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371000 // Earth radius in meters
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	return R * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// ConfigHandler godoc
//
//	@Summary	Get public client config
//	@Description	Returns the 2GIS map API key used by the mobile clients to render maps.
//	@Tags		config
//	@Produce	json
//	@Success	200	{object}	map[string]string
//	@Router		/config [get]
func (s *Server) ConfigHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"map_api_key": s.cfg.DoubleGISAPIKey})
}

// ListRidesHandler godoc
//
//	@Summary	List all active rides
//	@Description	Returns every ride with status pending or active. Used by passengers browsing available drivers.
//	@Tags		rides
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{array}		object{ride_id=string,route_id=string,start_lat=number,start_lon=number,finish_lat=number,finish_lon=number}
//	@Failure	401	{object}	map[string]string
//	@Failure	500	{object}	map[string]string
//	@Router		/rides [get]
func (s *Server) ListRidesHandler(c *gin.Context) {
	rides, err := s.db.ListActiveRides(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list rides"})
		slog.Error("failed to list rides", "error", err)
		return
	}
	type rideItem struct {
		RideID  string  `json:"ride_id"`
		RouteID string  `json:"route_id"`
		StartLat  float64 `json:"start_lat"`
		StartLon  float64 `json:"start_lon"`
		FinishLat float64 `json:"finish_lat"`
		FinishLon float64 `json:"finish_lon"`
	}
	items := make([]rideItem, len(rides))
	for i, r := range rides {
		items[i] = rideItem{
			RideID: r.RideID, RouteID: r.RouteID,
			StartLat: r.StartLat, StartLon: r.StartLon,
			FinishLat: r.FinishLat, FinishLon: r.FinishLon,
		}
	}
	c.JSON(http.StatusOK, items)
}

// GetRouteHandler godoc
//
//	@Summary	Get a route by id
//	@Description	Proxies to pathfinder; returns the driving route geometry and metadata.
//	@Tags		routes
//	@Security	BearerAuth
//	@Produce	json
//	@Param		id	path		string	true	"Route UUID"
//	@Success	200	{object}	map[string]any
//	@Failure	401	{object}	map[string]string
//	@Failure	502	{object}	map[string]string
//	@Router		/route/{id} [get]
func (s *Server) GetRouteHandler(c *gin.Context) {
	routeID := c.Param("id")
	route, err := s.pathfinder.GetRoute(c.Request.Context(), c.GetHeader("Authorization"), routeID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to get route: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, route)
}

// CreateDriverPathHandler godoc
//
//	@Summary	Create a driver path (ride)
//	@Description	Calls pathfinder to build the driving route, persists it as a pending ride, and returns the ride id. The caller's JWT must match `driver_id`.
//	@Tags		rides
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		CreateDriverPathRequest	true	"Driver id and start/finish coordinates"
//	@Success	201		{object}	CreateDriverPathResponse
//	@Failure	400		{object}	map[string]string
//	@Failure	401		{object}	map[string]string
//	@Failure	403		{object}	map[string]string
//	@Failure	500		{object}	map[string]string
//	@Failure	502		{object}	map[string]string
//	@Router		/ride/driver/path [post]
func (s *Server) CreateDriverPathHandler(c *gin.Context) {
	var req CreateDriverPathRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.DriverID != auth.GetUserID(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "user ID mismatch"})
		return
	}

	// Call pathfinder to create the driving route
	routeResp, err := s.pathfinder.CreateRoute(c.Request.Context(), c.GetHeader("Authorization"),
		req.Start.Lat, req.Start.Lon, req.Finish.Lat, req.Finish.Lon,
	)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to create route: " + err.Error()})
		return
	}

	// Build WKT LINESTRING from pathfinder segments
	wkt := segmentsToWKT(routeResp.Segments)
	if wkt == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "empty route geometry"})
		return
	}

	totalDuration := parseDuration(routeResp.TotalDuration)

	maxPass := req.MaxPassengers
	if maxPass <= 0 {
		maxPass = 3
	}
	mp := s.matchingParams.Get()
	if maxPass > mp.MaxPassengers {
		maxPass = mp.MaxPassengers
	}

	ride := &database.Ride{
		DriverID:      req.DriverID,
		RouteID:       routeResp.RouteID,
		GeometryWKT:   wkt,
		StartLat:      req.Start.Lat,
		StartLon:      req.Start.Lon,
		FinishLat:     req.Finish.Lat,
		FinishLon:     req.Finish.Lon,
		TotalDuration: totalDuration,
		MaxPassengers: maxPass,
		Status:        database.StatusPending,
	}

	if err := s.db.CreateRide(c.Request.Context(), ride); err != nil {
		slog.Error("orphaned route: pathfinder route created but local ride insert failed", "route_id", routeResp.RouteID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save ride"})
		return
	}

	slog.Info("ride created", "ride_id", ride.RideID, "route_id", routeResp.RouteID, "driver_id", req.DriverID)

	c.JSON(http.StatusCreated, CreateDriverPathResponse{
		RideID:  ride.RideID,
		RouteID: routeResp.RouteID,
		Status:  ride.Status,
	})
}

// MatchPassengerHandler godoc
//
//	@Summary	Match a passenger to a driver (REST)
//	@Description	Runs the PostGIS spatial search, filters by capacity and driver position, picks the best candidate, computes the walking path to the pickup point, and creates a pending sub-ride. Synchronous alternative to the Socket.IO offer flow.
//	@Tags		matching
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		MatchPassengerRequest	true	"Passenger id and pickup/dropoff coordinates"
//	@Success	201		{object}	MatchPassengerResponse
//	@Failure	400		{object}	map[string]string
//	@Failure	401		{object}	map[string]string
//	@Failure	403		{object}	map[string]string
//	@Failure	404		{object}	map[string]string
//	@Failure	500		{object}	map[string]string
//	@Router		/ride/passenger/match [post]
func (s *Server) MatchPassengerHandler(c *gin.Context) {
	var req MatchPassengerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.PassengerID != auth.GetUserID(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "user ID mismatch"})
		return
	}

	mp := s.matchingParams.Get()

	candidates, err := s.db.FindMatchingRides(c.Request.Context(), database.MatchParams{
		PickupLon:     req.Pickup.Lon,
		PickupLat:     req.Pickup.Lat,
		DropoffLon:    req.Dropoff.Lon,
		DropoffLat:    req.Dropoff.Lat,
		PickupRadius:  mp.PickupRadiusM,
		DropoffRadius: mp.DropoffRadiusM,
		MaxResults:    10,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "matching query failed"})
		slog.Error("matching query failed", "passenger_id", req.PassengerID, "error", err)
		return
	}

	var match *database.MatchResult
	for i := range candidates {
		cand := &candidates[i]

		count, err := s.db.CountActiveSubRides(c.Request.Context(), cand.RideID)
		if err != nil {
			slog.Error("failed to count sub-rides", "ride_id", cand.RideID, "error", err)
			continue
		}
		if count >= cand.MaxPassengers {
			continue
		}

		// Route-based check: has driver already passed the pickup?
		// Only runs for active rides — pending rides haven't started driving,
		// so the driver's current GPS is meaningless for accept/reject here.
		status, statusErr := s.db.GetRideStatus(c.Request.Context(), cand.RideID)
		if statusErr != nil {
			slog.Warn("failed to fetch ride status for position filter — proceeding with offer",
				"ride_id", cand.RideID, "error", statusErr)
		} else if status == database.StatusActive {
			loc, locErr := s.location.GetCurrentLocation(c.Request.Context(), cand.DriverID)
			if locErr == nil {
				driverFrac, fracErr := s.db.GetDriverFraction(c.Request.Context(), cand.RideID, loc.Lon, loc.Lat)
				if fracErr == nil && driverFrac >= cand.PickupFrac {
					slog.Info("driver already passed pickup, skipping",
						"driver_id", cand.DriverID, "driver_frac", driverFrac, "pickup_frac", cand.PickupFrac)
					continue
				}
			}
		}

		match = cand
		break
	}

	if match == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no matching ride found"})
		return
	}

	// Get walking directions from pathfinder
	var walkPath *WalkPath
	walkResp, err := s.pathfinder.WalkToClosest(c.Request.Context(), c.GetHeader("Authorization"),
		match.RouteID, req.Pickup.Lat, req.Pickup.Lon,
	)
	if err != nil {
		slog.Warn("walk-to-closest failed", "ride_id", match.RideID, "error", err)
	} else {
		coords := make([]WalkCoord, len(walkResp.Geometry))
		for i, p := range walkResp.Geometry {
			coords[i] = WalkCoord{Lon: p.Lon, Lat: p.Lat}
		}
		walkPath = &WalkPath{
			Geometry:     coords,
			WalkDistance: walkResp.WalkDistance,
			WalkDuration: walkResp.WalkDuration,
		}
	}

	subRide := &database.SubRide{
		RideID:      match.RideID,
		PassengerID: req.PassengerID,
		PickupLat:   req.Pickup.Lat,
		PickupLon:   req.Pickup.Lon,
		DropoffLat:  req.Dropoff.Lat,
		DropoffLon:  req.Dropoff.Lon,
		StartLat:    match.PickupPointLat,
		StartLon:    match.PickupPointLon,
		FinishLat:   match.DropoffPointLat,
		FinishLon:   match.DropoffPointLon,
		Status:      database.StatusPending,
	}

	if err := s.db.CreateSubRide(c.Request.Context(), subRide); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create sub-ride"})
		slog.Error("failed to create sub-ride", "ride_id", match.RideID, "passenger_id", req.PassengerID, "error", err)
		return
	}

	slog.Info("passenger matched", "ride_id", match.RideID, "passenger_id", req.PassengerID, "driver_id", match.DriverID, "sub_ride_id", subRide.SubRideID)

	c.JSON(http.StatusCreated, MatchPassengerResponse{
		SubRideID:    subRide.SubRideID,
		RideID:       match.RideID,
		DriverID:     match.DriverID,
		PickupPoint:  Coord{Lat: match.PickupPointLat, Lon: match.PickupPointLon},
		DropoffPoint: Coord{Lat: match.DropoffPointLat, Lon: match.DropoffPointLon},
		PickupWalkM:  match.PickupWalkM,
		DropoffWalkM: match.DropoffWalkM,
		TotalWalkM:   match.PickupWalkM + match.DropoffWalkM,
		WalkToPickup: walkPath,
	})
}


func segmentsToWKT(segments []pathfinder.Segment) string {
	var points []string
	for _, seg := range segments {
		for _, coord := range seg.Coords {
			points = append(points, fmt.Sprintf("%f %f", coord.Lon, coord.Lat))
		}
	}
	if len(points) < 2 {
		return ""
	}
	return "LINESTRING(" + strings.Join(points, ",") + ")"
}

// parseDuration converts pathfinder duration strings like "12 min", "1 h", or "1 h 5 min" to seconds.
func parseDuration(s string) int {
	s = strings.TrimSpace(s)
	var hours, mins int
	if n, _ := fmt.Sscanf(s, "%d h %d min", &hours, &mins); n == 2 {
		return hours*3600 + mins*60
	}
	if n, _ := fmt.Sscanf(s, "%d h", &hours); n == 1 {
		return hours * 3600
	}
	if n, _ := fmt.Sscanf(s, "%d min", &mins); n == 1 {
		return mins * 60
	}
	slog.Warn("unrecognized duration format", "duration", s)
	return 0
}

// SetDriverLocationHandler godoc
//
//	@Summary	Update driver location (REST)
//	@Description	Writes the driver's current GPS coordinates to the location service. Caller's JWT must match `driver_id`. Prefer the Socket.IO `location_update` event for high-frequency updates.
//	@Tags		location
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		SetDriverLocationRequest	true	"Driver id and coordinates"
//	@Success	200		{object}	map[string]string
//	@Failure	400		{object}	map[string]string
//	@Failure	401		{object}	map[string]string
//	@Failure	403		{object}	map[string]string
//	@Failure	502		{object}	map[string]string
//	@Router		/driver/location [post]
func (s *Server) SetDriverLocationHandler(c *gin.Context) {
	var req SetDriverLocationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.DriverID != auth.GetUserID(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "user ID mismatch"})
		return
	}

	if err := s.location.SetLocation(c.Request.Context(), req.DriverID, "driver", req.Lat, req.Lon); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to set location: " + err.Error()})
		slog.Error("failed to set driver location", "driver_id", req.DriverID, "error", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// SwaggerJSONHandler godoc
//
//	@Summary	Get the OpenAPI 2.0 spec for this service
//	@Description	Returns the generated swagger.json. The `host` and `basePath` fields can be overridden at runtime via SWAGGER_HOST and SWAGGER_BASE_PATH env vars (useful when running behind the API gateway).
//	@Tags		meta
//	@Produce	json
//	@Success	200	{object}	map[string]any
//	@Failure	500	{object}	map[string]string
//	@Router		/docs/swagger.json [get]
func (s *Server) SwaggerJSONHandler(c *gin.Context) {
	data, err := os.ReadFile("./docs/swagger.json")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "swagger spec not found"})
		return
	}
	var spec map[string]any
	if err := json.Unmarshal(data, &spec); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid swagger spec"})
		return
	}
	if h := os.Getenv("SWAGGER_HOST"); h != "" {
		spec["host"] = h
	}
	if bp := os.Getenv("SWAGGER_BASE_PATH"); bp != "" {
		spec["basePath"] = bp
	}
	c.JSON(http.StatusOK, spec)
}

// RideHistoryHandler godoc
//
//	@Summary	Get the caller's ride history
//	@Description	Returns paginated past rides for the authenticated user. Drivers see ride-level entries (with passenger count and total earnings); passengers see sub-ride-level entries (with the driver and price). The shape of `rides` depends on the caller's role.
//	@Tags		rides
//	@Security	BearerAuth
//	@Produce	json
//	@Param		page	query		int	false	"Page number, 1-based"			default(1)	minimum(1)
//	@Param		limit	query		int	false	"Page size (max 100)"			default(20)	minimum(1)	maximum(100)
//	@Success	200		{object}	HistoryResponse
//	@Failure	401		{object}	map[string]string
//	@Failure	500		{object}	map[string]string
//	@Router		/rides/history [get]
func (s *Server) RideHistoryHandler(c *gin.Context) {
	userID := auth.GetUserID(c)
	role := auth.GetRole(c)

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	offset := (page - 1) * limit

	ctx := c.Request.Context()

	if role == "driver" {
		rows, total, err := s.db.GetDriverRideHistory(ctx, userID, limit, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get history"})
			slog.Error("failed to get driver ride history", "user_id", userID, "error", err)
			return
		}
		items := make([]DriverHistoryItem, len(rows))
		for i, r := range rows {
			items[i] = DriverHistoryItem{
				RideID: r.RideID, RouteID: r.RouteID, Status: r.Status,
				MaxPassengers: r.MaxPassengers,
				StartLat: r.StartLat, StartLon: r.StartLon,
				FinishLat: r.FinishLat, FinishLon: r.FinishLon,
				TotalDuration: r.TotalDuration, PassengerCount: r.PassengerCount,
				TotalEarnings: r.TotalEarnings,
				StartedAt: r.StartedAt, CompletedAt: r.CompletedAt, CreatedAt: r.CreatedAt,
			}
		}
		c.JSON(http.StatusOK, HistoryResponse{Rides: items, Total: total, Page: page, Limit: limit})
		return
	}

	// Default: passenger
	rows, total, err := s.db.GetPassengerRideHistory(ctx, userID, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get history"})
		slog.Error("failed to get passenger ride history", "user_id", userID, "error", err)
		return
	}
	items := make([]PassengerHistoryItem, len(rows))
	for i, r := range rows {
		items[i] = PassengerHistoryItem{
			SubRideID: r.SubRideID, RideID: r.RideID, DriverID: r.DriverID,
			Status: r.Status, Price: r.Price,
			PickupLat: r.PickupLat, PickupLon: r.PickupLon,
			DropoffLat: r.DropoffLat, DropoffLon: r.DropoffLon,
			CompletedAt: r.CompletedAt, CreatedAt: r.CreatedAt,
		}
	}
	c.JSON(http.StatusOK, HistoryResponse{Rides: items, Total: total, Page: page, Limit: limit})
}
