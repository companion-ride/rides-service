package server

import (
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func (s *Server) RegisterRoutes() http.Handler {
	r := gin.Default()

	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowHeaders:     []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
	}))

	r.GET("/", s.HelloWorldHandler)
	r.GET("/health", s.healthHandler)

	r.StaticFile("/driver", "./web/driver.html")
	r.StaticFile("/passenger", "./web/passenger.html")
	r.GET("/api/config", s.ConfigHandler)
	r.GET("/api/route/:id", s.GetRouteHandler)
	r.GET("/api/rides", s.ListRidesHandler)

	ride := r.Group("/ride")
	{
		ride.POST("/driver/path", s.CreateDriverPathHandler)
		ride.POST("/passenger/match", s.MatchPassengerHandler)
	}

	return r
}

func (s *Server) HelloWorldHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "Hello World"})
}

func (s *Server) healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, s.db.Health())
}
