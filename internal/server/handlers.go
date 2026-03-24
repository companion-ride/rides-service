package server

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
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

func (s *Server) ConfigHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"map_api_key": s.cfg.DoubleGISAPIKey})
}

func (s *Server) ListRidesHandler(c *gin.Context) {
	rides, err := s.db.ListActiveRides(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list rides"})
		log.Printf("list rides error: %v", err)
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

func (s *Server) GetRouteHandler(c *gin.Context) {
	routeID := c.Param("id")
	route, err := s.pathfinder.GetRoute(c.Request.Context(), c.GetHeader("Authorization"), routeID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to get route: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, route)
}

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

	ride := &database.Ride{
		DriverID:      req.DriverID,
		RouteID:       routeResp.RouteID,
		GeometryWKT:   wkt,
		StartLat:      req.Start.Lat,
		StartLon:      req.Start.Lon,
		FinishLat:     req.Finish.Lat,
		FinishLon:     req.Finish.Lon,
		TotalDuration: totalDuration,
		Status:        database.StatusPending,
	}

	if err := s.db.CreateRide(c.Request.Context(), ride); err != nil {
		log.Printf("ORPHANED ROUTE: pathfinder route %s created but local ride insert failed: %v", routeResp.RouteID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save ride"})
		return
	}

	c.JSON(http.StatusCreated, CreateDriverPathResponse{
		RideID:  ride.RideID,
		RouteID: routeResp.RouteID,
		Status:  ride.Status,
	})
}

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
		log.Printf("matching query error: %v", err)
		return
	}

	var match *database.MatchResult
	for i := range candidates {
		cand := &candidates[i]

		count, err := s.db.CountActiveSubRides(c.Request.Context(), cand.RideID)
		if err != nil {
			log.Printf("count sub-rides error: %v", err)
			continue
		}
		if count >= mp.MaxPassengers {
			continue
		}

		// Distance check: skip if driver is too close to pickup
		loc, locErr := s.location.GetCurrentLocation(c.Request.Context(), cand.DriverID)
		if locErr == nil {
			dist := haversine(loc.Lat, loc.Lon, cand.PickupPointLat, cand.PickupPointLon)
			if dist < mp.MinDriverDistanceM {
				log.Printf("driver %s too close (%.0fm < %.0fm), skipping", cand.DriverID, dist, mp.MinDriverDistanceM)
				continue
			}

			// Route-based check: has driver already passed the pickup?
			driverFrac, fracErr := s.db.GetDriverFraction(c.Request.Context(), cand.RideID, loc.Lon, loc.Lat)
			if fracErr == nil {
				if driverFrac >= cand.PickupFrac {
					log.Printf("driver %s already passed pickup (frac %.3f >= %.3f), skipping",
						cand.DriverID, driverFrac, cand.PickupFrac)
					continue
				}
				etaSeconds := (cand.PickupFrac - driverFrac) * float64(cand.TotalDuration)
				if etaSeconds < float64(mp.MinETASeconds) {
					log.Printf("driver %s ETA too short (%.0fs < %ds), skipping",
						cand.DriverID, etaSeconds, mp.MinETASeconds)
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
		log.Printf("walk-to-closest failed (non-fatal): %v", err)
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
		log.Printf("create sub-ride error: %v", err)
		return
	}

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
	if len(points) == 0 {
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
	log.Printf("unrecognized duration format: %q", s)
	return 0
}

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
		log.Printf("set driver location error: %v", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

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
