package server

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"rides/internal/database"
	"rides/internal/pathfinder"
)

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
	route, err := s.pathfinder.GetRoute(c.Request.Context(), routeID)
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

	// Call pathfinder to create the driving route
	routeResp, err := s.pathfinder.CreateRoute(c.Request.Context(),
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
		Status:        "pending",
	}

	if err := s.db.CreateRide(c.Request.Context(), ride); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save ride"})
		log.Printf("create ride error: %v", err)
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

	candidates, err := s.db.FindMatchingRides(c.Request.Context(), database.MatchParams{
		PickupLon:     req.Pickup.Lon,
		PickupLat:     req.Pickup.Lat,
		DropoffLon:    req.Dropoff.Lon,
		DropoffLat:    req.Dropoff.Lat,
		PickupRadius:  s.cfg.PickupRadiusM,
		DropoffRadius: s.cfg.DropoffRadiusM,
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
		if count >= s.cfg.MaxPassengers {
			continue
		}

		if cand.Status == "active" {
			if !s.checkETA(c, cand) {
				continue
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
	walkResp, err := s.pathfinder.WalkToClosest(c.Request.Context(),
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
		Status:      "pending",
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

func (s *Server) checkETA(c *gin.Context, cand *database.MatchResult) bool {
	loc, err := s.location.GetCurrentLocation(c.Request.Context(), cand.DriverID)
	if err != nil {
		log.Printf("location service error (skipping ETA check): %v", err)
		return true
	}

	driverFrac, err := s.db.GetDriverFraction(c.Request.Context(), cand.RideID, loc.Lon, loc.Lat)
	if err != nil {
		log.Printf("driver fraction query error: %v", err)
		return true
	}

	if driverFrac >= cand.PickupFrac {
		return false
	}

	etaSeconds := (cand.PickupFrac - driverFrac) * float64(cand.TotalDuration)
	return etaSeconds >= float64(s.cfg.MinETASeconds)
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

// parseDuration converts pathfinder duration strings like "12 min" or "1 h 5 min" to seconds.
func parseDuration(s string) int {
	s = strings.TrimSpace(s)
	var hours, mins int
	if n, _ := fmt.Sscanf(s, "%d h %d min", &hours, &mins); n == 2 {
		return hours*3600 + mins*60
	}
	if n, _ := fmt.Sscanf(s, "%d min", &mins); n == 1 {
		return mins * 60
	}
	return 0
}
