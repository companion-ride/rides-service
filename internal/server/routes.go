package server

import (
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"rides/internal/auth"
)

func (s *Server) RegisterRoutes() http.Handler {
	r := gin.Default()

	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173", "https://companion.kopir.uk"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowHeaders:     []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
	}))

	// Public routes
	r.GET("/", s.HelloWorldHandler)
	r.GET("/health", s.healthHandler)
	r.StaticFile("/driver", "./web/driver.html")
	r.StaticFile("/passenger", "./web/passenger.html")
	r.StaticFile("/docs", "./web/swagger.html")
	r.GET("/docs/swagger.json", s.SwaggerJSONHandler)
	r.GET("/config", s.ConfigHandler)

	// Protected routes
	protected := r.Group("/")
	protected.Use(auth.Middleware(s.cfg.JWTSecretKey))
	protected.GET("/route/:id", s.GetRouteHandler)
	protected.GET("/rides", s.ListRidesHandler)
	protected.POST("/driver/location", s.SetDriverLocationHandler)

	protectedRide := protected.Group("/ride")
	protectedRide.POST("/driver/path", s.CreateDriverPathHandler)
	protectedRide.POST("/passenger/match", s.MatchPassengerHandler)

	return r
}

func (s *Server) HelloWorldHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "Hello World"})
}

func (s *Server) healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, s.db.Health())
}
