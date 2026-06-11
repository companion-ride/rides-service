package server

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"rides/internal/auth"
)

func (s *Server) RegisterRoutes() http.Handler {
	r := gin.Default()

	origins := []string{"http://localhost:5173", "https://companion.kopir.uk"}
	if env := os.Getenv("CORS_ORIGINS"); env != "" {
		origins = strings.Split(env, ",")
	}
	r.Use(cors.New(cors.Config{
		AllowOrigins:     origins,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowHeaders:     []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
	}))

	// Public routes
	r.GET("/", s.HelloWorldHandler)
	r.GET("/health", s.healthHandler)
	r.StaticFile("/docs", "./web/swagger.html")
	r.GET("/docs/swagger.json", s.SwaggerJSONHandler)

	// Debug pages and the map key they read; off unless explicitly enabled.
	if enabled, _ := strconv.ParseBool(os.Getenv("ENABLE_DEBUG_PAGES")); enabled {
		r.StaticFile("/driver", "./web/driver.html")
		r.StaticFile("/passenger", "./web/passenger.html")
		r.GET("/config", s.ConfigHandler)
	}

	// Protected routes
	protected := r.Group("/")
	protected.Use(auth.Middleware(s.cfg.JWTSecretKey))
	protected.GET("/route/:id", s.GetRouteHandler)
	protected.GET("/rides", s.ListRidesHandler)
	protected.POST("/driver/location", s.SetDriverLocationHandler)

	protectedRide := protected.Group("/ride")
	protectedRide.POST("/driver/path", s.CreateDriverPathHandler)
	protectedRide.POST("/driver/custom-path", s.CreateDriverCustomPathHandler)
	protectedRide.POST("/passenger/match", s.MatchPassengerHandler)

	protected.GET("/rides/history", s.RideHistoryHandler)

	// Admin routes (JWT + admin role required)
	admin := protected.Group("/admin")
	admin.Use(auth.RequireRole("admin"))
	admin.GET("/rides", s.AdminListRidesHandler)
	admin.GET("/rides/:id", s.AdminGetRideHandler)
	admin.GET("/matching/stats", s.AdminMatchingStatsHandler)
	admin.GET("/matching/params", s.AdminGetMatchingParamsHandler)
	admin.PUT("/matching/params", s.AdminUpdateMatchingParamsHandler)
	admin.GET("/settings", s.AdminGetSettingsHandler)
	admin.PUT("/settings", s.AdminUpdateSettingsHandler)
	admin.GET("/drivers/active", s.AdminActiveDriversHandler)
	admin.DELETE("/rides", s.AdminDeleteAllRidesHandler)

	return r
}

// HelloWorldHandler godoc
//
//	@Summary	Liveness ping
//	@Description	Returns a static hello payload. Useful as a smoke test that the service is reachable.
//	@Tags		meta
//	@Produce	json
//	@Success	200	{object}	map[string]string
//	@Router		/ [get]
func (s *Server) HelloWorldHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "Hello World"})
}

// healthHandler godoc
//
//	@Summary	Health check
//	@Description	Returns database connection pool stats and a top-level status (up/down). Used by Docker healthchecks and the gateway's health filter.
//	@Tags		meta
//	@Produce	json
//	@Success	200	{object}	map[string]string
//	@Router		/health [get]
func (s *Server) healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, s.db.Health())
}
