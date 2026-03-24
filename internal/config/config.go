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
	DoubleGISAPIKey    string
	JWTSecretKey       string
}

func Load() *Config {
	port, _ := strconv.Atoi(os.Getenv("PORT"))

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
		DoubleGISAPIKey:    os.Getenv("DOUBLE_GIS_API_KEY"),
		JWTSecretKey:       os.Getenv("JWT_SECRET_KEY"),
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
