package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"rides/internal/config"
	"rides/internal/database"
)

func (s *Server) AdminGetSettingsHandler(c *gin.Context) {
	settings, err := s.db.GetAdminSettings(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get settings"})
		slog.Error("failed to get admin settings", "error", err)
		return
	}
	c.Data(http.StatusOK, "application/json", settings)
}

func (s *Server) AdminUpdateSettingsHandler(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 64*1024)) // 64KB max
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}
	if !json.Valid(body) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON"})
		return
	}
	if err := s.db.UpsertAdminSettings(c.Request.Context(), body); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save settings"})
		slog.Error("failed to upsert admin settings", "error", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) AdminGetMatchingParamsHandler(c *gin.Context) {
	p := s.matchingParams.Get()
	c.JSON(http.StatusOK, MatchingParamsResponse{
		PickupRadiusM:      p.PickupRadiusM,
		DropoffRadiusM:     p.DropoffRadiusM,
		MaxPassengers:      p.MaxPassengers,
		MinEtaSeconds:      p.MinETASeconds,
		MinDriverDistanceM: p.MinDriverDistanceM,
	})
}

func (s *Server) AdminUpdateMatchingParamsHandler(c *gin.Context) {
	var req MatchingParamsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	data, err := json.Marshal(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to marshal params"})
		return
	}
	if err := s.db.UpsertMatchingParams(c.Request.Context(), data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save params"})
		slog.Error("failed to upsert matching params", "error", err)
		return
	}

	// Immediately update in-memory params
	s.matchingParams.Store(&config.MatchingParams{
		PickupRadiusM:      float64(req.PickupRadiusM),
		DropoffRadiusM:     float64(req.DropoffRadiusM),
		MaxPassengers:      req.MaxPassengers,
		MinETASeconds:      req.MinEtaSeconds,
		MinDriverDistanceM: float64(req.MinDriverDistanceM),
	})

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) AdminListRidesHandler(c *gin.Context) {
	page := queryIntOrDefault(c, "page", 1)
	limit := queryIntOrDefault(c, "limit", 20)
	if limit > 100 {
		limit = 100
	}
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * limit

	rides, total, err := s.db.ListAdminRides(c.Request.Context(), database.AdminListParams{
		Status: c.Query("status"),
		Search: c.Query("search"),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list rides"})
		slog.Error("failed to list admin rides", "error", err)
		return
	}

	items := make([]AdminRideListItem, len(rides))
	for i, r := range rides {
		items[i] = AdminRideListItem{
			ID:            r.RideID,
			DriverID:      r.DriverID,
			FromAddress:   r.FromAddress,
			ToAddress:     r.ToAddress,
			Status:        r.Status,
			Price:         r.Price,
			DistanceM:     r.DistanceM,
			TotalDuration: r.TotalDuration,
			PaymentMethod: r.PaymentMethod,
			Passengers:    r.Passengers,
			CreatedAt:     r.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, AdminRideListResponse{
		Rides: items,
		Total: total,
		Page:  page,
		Limit: limit,
	})
}

func (s *Server) AdminGetRideHandler(c *gin.Context) {
	rideID := c.Param("id")
	ride, subs, err := s.db.GetAdminRideDetail(c.Request.Context(), rideID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "ride not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get ride"})
			slog.Error("failed to get admin ride detail", "ride_id", rideID, "error", err)
		}
		return
	}

	subItems := make([]AdminSubRideItem, len(subs))
	for i, sr := range subs {
		subItems[i] = AdminSubRideItem{
			SubRideID:   sr.SubRideID,
			PassengerID: sr.PassengerID,
			PickupLat:   sr.PickupLat,
			PickupLon:   sr.PickupLon,
			DropoffLat:  sr.DropoffLat,
			DropoffLon:  sr.DropoffLon,
			Status:      sr.Status,
			CompletedAt: sr.CompletedAt,
			CancelledAt: sr.CancelledAt,
			CreatedAt:   sr.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, AdminRideDetailResponse{
		ID:            ride.RideID,
		DriverID:      ride.DriverID,
		RouteID:       ride.RouteID,
		FromAddress:   ride.FromAddress,
		ToAddress:     ride.ToAddress,
		Status:        ride.Status,
		Price:         ride.Price,
		DistanceM:     ride.DistanceM,
		TotalDuration: ride.TotalDuration,
		PaymentMethod: ride.PaymentMethod,
		StartLat:      ride.StartLat,
		StartLon:      ride.StartLon,
		FinishLat:     ride.FinishLat,
		FinishLon:     ride.FinishLon,
		StartedAt:     ride.StartedAt,
		CompletedAt:   ride.CompletedAt,
		CancelledAt:   ride.CancelledAt,
		CreatedAt:     ride.CreatedAt,
		SubRides:      subItems,
	})
}

func (s *Server) AdminMatchingStatsHandler(c *gin.Context) {
	stats, err := s.db.GetMatchingStats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get stats"})
		slog.Error("failed to get matching stats", "error", err)
		return
	}

	var matchRate, avgPPR, cancRate float64
	if stats.TotalRides > 0 {
		matchRate = float64(stats.MatchedRides) / float64(stats.TotalRides) * 100
		cancRate = float64(stats.CancelledRides) / float64(stats.TotalRides) * 100
	}
	if stats.MatchedRides > 0 {
		avgPPR = float64(stats.TotalSubRides) / float64(stats.MatchedRides)
	}

	c.JSON(http.StatusOK, MatchingStatsResponse{
		TotalRides:           stats.TotalRides,
		RidesByStatus:        stats.RidesByStatus,
		MatchRate:            matchRate,
		AvgWaitSeconds:       stats.AvgWaitSeconds,
		TotalSubRides:        stats.TotalSubRides,
		AvgPassengersPerRide: avgPPR,
		CancellationRate:     cancRate,
	})
}

func (s *Server) AdminActiveDriversHandler(c *gin.Context) {
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet,
		s.cfg.LocationServiceURL+"/admin/drivers/active", nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create request"})
		return
	}
	// Forward the Authorization header
	req.Header.Set("Authorization", c.GetHeader("Authorization"))

	resp, err := s.location.ProxyRequest(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "location service unavailable"})
		slog.Error("location service unavailable for active drivers", "error", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read location service response"})
		slog.Error("failed to read location service response", "error", err)
		return
	}
	c.Data(resp.StatusCode, "application/json", body)
}

func queryIntOrDefault(c *gin.Context, key string, def int) int {
	v := c.Query(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
