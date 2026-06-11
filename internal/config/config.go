package config

import (
	"log/slog"
	"os"
	"strconv"
	"strings"

	_ "github.com/joho/godotenv/autoload"
)

type Config struct {
	Port                   int
	DBHost                 string
	DBPort                 string
	DBDatabase             string
	DBUsername             string
	DBPassword             string
	DBSchema               string
	PathfinderURL          string
	LocationServiceURL     string
	DoubleGISAPIKey        string
	JWTSecretKey           string
	KafkaBootstrapServers  string
	InternalAPIKey         string
}

func Load() *Config {
	port, err := strconv.Atoi(os.Getenv("PORT"))
	if err != nil || port == 0 {
		port = 8081
		if os.Getenv("PORT") != "" {
			slog.Warn("invalid PORT, using default", "port_value", os.Getenv("PORT"), "default", port)
		}
	}

	// Validate critical environment variables
	for _, key := range []string{"JWT_SECRET_KEY", "DB_HOST", "DB_PORT", "DB_DATABASE", "DB_USERNAME"} {
		if os.Getenv(key) == "" {
			slog.Error("required environment variable is missing", "key", key)
			os.Exit(1)
		}
	}

	return &Config{
		Port:                   port,
		DBHost:                 os.Getenv("DB_HOST"),
		DBPort:                 os.Getenv("DB_PORT"),
		DBDatabase:             os.Getenv("DB_DATABASE"),
		DBUsername:             os.Getenv("DB_USERNAME"),
		DBPassword:             os.Getenv("DB_PASSWORD"),
		DBSchema:               os.Getenv("DB_SCHEMA"),
		PathfinderURL:          os.Getenv("PATHFINDER_URL"),
		LocationServiceURL:     os.Getenv("LOCATION_SERVICE_URL"),
		DoubleGISAPIKey:        os.Getenv("DOUBLE_GIS_API_KEY"),
		JWTSecretKey:           os.Getenv("JWT_SECRET_KEY"),
		KafkaBootstrapServers:  envStringOrDefault("KAFKA_BOOTSTRAP_SERVERS", "localhost:9092"),
		InternalAPIKey:         strings.TrimSpace(os.Getenv("INTERNAL_API_KEY")),
	}
}

func envStringOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
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
