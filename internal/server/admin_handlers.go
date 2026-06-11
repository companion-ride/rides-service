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

// AdminGetSettingsHandler godoc
//
//	@Summary	Get admin settings (raw JSON)
//	@Description	Returns the free-form JSON document holding feature flags and tunables. Schema is admin-panel-defined and not enforced server-side. Admin role required.
//	@Tags		admin
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{object}	map[string]any
//	@Failure	401	{object}	map[string]string
//	@Failure	403	{object}	map[string]string
//	@Failure	500	{object}	map[string]string
//	@Router		/admin/settings [get]
func (s *Server) AdminGetSettingsHandler(c *gin.Context) {
	settings, err := s.db.GetAdminSettings(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get settings"})
		slog.Error("failed to get admin settings", "error", err)
		return
	}
	c.Data(http.StatusOK, "application/json", settings)
}

// AdminUpdateSettingsHandler godoc
//
//	@Summary	Upsert admin settings (raw JSON)
//	@Description	Stores a free-form JSON document. Body must be valid JSON, max 64 KB. Admin role required.
//	@Tags		admin
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		object	true	"Arbitrary JSON"
//	@Success	200		{object}	map[string]string
//	@Failure	400		{object}	map[string]string
//	@Failure	401		{object}	map[string]string
//	@Failure	403		{object}	map[string]string
//	@Failure	500		{object}	map[string]string
//	@Router		/admin/settings [put]
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

// AdminGetMatchingParamsHandler godoc
//
//	@Summary	Get current matching parameters
//	@Description	Returns the active in-memory matching parameters (pickup radius, dropoff radius, max passengers, min ETA, min driver distance). Admin role required.
//	@Tags		admin
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{object}	MatchingParamsResponse
//	@Failure	401	{object}	map[string]string
//	@Failure	403	{object}	map[string]string
//	@Router		/admin/matching/params [get]
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

// AdminUpdateMatchingParamsHandler godoc
//
//	@Summary	Update matching parameters
//	@Description	Persists new parameters to the admin_settings table and swaps the in-memory atomic pointer so the change takes effect immediately for new requests. Admin role required.
//	@Tags		admin
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		MatchingParamsRequest	true	"New parameter values"
//	@Success	200		{object}	map[string]string
//	@Failure	400		{object}	map[string]string
//	@Failure	401		{object}	map[string]string
//	@Failure	403		{object}	map[string]string
//	@Failure	500		{object}	map[string]string
//	@Router		/admin/matching/params [put]
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

// AdminListRidesHandler godoc
//
//	@Summary	List rides (admin view)
//	@Description	Paginated, filterable list of rides for the admin panel. Filters: status (pending/active/completed/cancelled) and search (free-text against driver id or addresses). Admin role required.
//	@Tags		admin
//	@Security	BearerAuth
//	@Produce	json
//	@Param		page	query		int		false	"Page number, 1-based"			default(1)	minimum(1)
//	@Param		limit	query		int		false	"Page size (max 100)"			default(20)	minimum(1)	maximum(100)
//	@Param		status	query		string	false	"Filter by ride status"			Enums(pending, active, completed, cancelled)
//	@Param		search	query		string	false	"Free-text search"
//	@Success	200		{object}	AdminRideListResponse
//	@Failure	401		{object}	map[string]string
//	@Failure	403		{object}	map[string]string
//	@Failure	500		{object}	map[string]string
//	@Router		/admin/rides [get]
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

// AdminGetRideHandler godoc
//
//	@Summary	Get a ride's full detail (admin view)
//	@Description	Returns the ride and all of its sub-rides, including cancelled and completed ones. Admin role required.
//	@Tags		admin
//	@Security	BearerAuth
//	@Produce	json
//	@Param		id	path		string	true	"Ride UUID"
//	@Success	200	{object}	AdminRideDetailResponse
//	@Failure	401	{object}	map[string]string
//	@Failure	403	{object}	map[string]string
//	@Failure	404	{object}	map[string]string
//	@Failure	500	{object}	map[string]string
//	@Router		/admin/rides/{id} [get]
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

// AdminMatchingStatsHandler godoc
//
//	@Summary	Get matching pipeline statistics
//	@Description	Returns aggregated counts (total rides, by-status breakdown, total sub-rides) and derived ratios (match rate, average passengers per matched ride, cancellation rate, average wait seconds). Admin role required.
//	@Tags		admin
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{object}	MatchingStatsResponse
//	@Failure	401	{object}	map[string]string
//	@Failure	403	{object}	map[string]string
//	@Failure	500	{object}	map[string]string
//	@Router		/admin/matching/stats [get]
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

// AdminActiveDriversHandler godoc
//
//	@Summary	List currently active drivers (proxy)
//	@Description	Proxies to the location service's `/admin/drivers/active` endpoint and returns its response verbatim. Admin role required.
//	@Tags		admin
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{object}	map[string]any
//	@Failure	401	{object}	map[string]string
//	@Failure	403	{object}	map[string]string
//	@Failure	502	{object}	map[string]string
//	@Router		/admin/drivers/active [get]
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

// AdminDeleteAllRidesHandler godoc
//
//	@Summary	Delete every ride (destructive)
//	@Description	Wipes the rides table. Used during development and integration tests; admin role required.
//	@Tags		admin
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{object}	map[string]string
//	@Failure	401	{object}	map[string]string
//	@Failure	403	{object}	map[string]string
//	@Failure	500	{object}	map[string]string
//	@Router		/admin/rides [delete]
func (s *Server) AdminDeleteAllRidesHandler(c *gin.Context) {
	ctx := c.Request.Context()
	if _, err := s.db.DeleteAllRides(ctx); err != nil {
		slog.Error("failed to delete all rides", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete rides"})
		return
	}
	slog.Info("all rides deleted by admin")
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
