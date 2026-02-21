package config

import (
	"os"
	"strconv"

	_ "github.com/joho/godotenv/autoload"
)

type Config struct {
	Port               int
	DBHost             string
	DBPort             string
	DBDatabase         string
	DBUsername         string
	DBPassword         string
	DBSchema           string
	PathfinderURL      string
	LocationServiceURL string
	MaxPassengers      int
	PickupRadiusM      float64
	DropoffRadiusM     float64
	MinETASeconds      int
}

func Load() *Config {
	port, _ := strconv.Atoi(os.Getenv("PORT"))
	maxPassengers := envIntOrDefault("MAX_PASSENGERS", 3)
	pickupRadius := envFloatOrDefault("PICKUP_RADIUS_M", 500)
	dropoffRadius := envFloatOrDefault("DROPOFF_RADIUS_M", 500)
	minETA := envIntOrDefault("MIN_ETA_SECONDS", 180)

	return &Config{
		Port:               port,
		DBHost:             os.Getenv("DB_HOST"),
		DBPort:             os.Getenv("DB_PORT"),
		DBDatabase:         os.Getenv("DB_DATABASE"),
		DBUsername:         os.Getenv("DB_USERNAME"),
		DBPassword:         os.Getenv("DB_PASSWORD"),
		DBSchema:           os.Getenv("DB_SCHEMA"),
		PathfinderURL:      os.Getenv("PATHFINDER_URL"),
		LocationServiceURL: os.Getenv("LOCATION_SERVICE_URL"),
		MaxPassengers:      maxPassengers,
		PickupRadiusM:      pickupRadius,
		DropoffRadiusM:     dropoffRadius,
		MinETASeconds:      minETA,
	}
}

func envIntOrDefault(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envFloatOrDefault(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}
