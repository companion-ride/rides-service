package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"rides/internal/config"
	"rides/internal/database"
	kafkapkg "rides/internal/kafka"
	"rides/internal/location"
	"rides/internal/matching"
	"rides/internal/server"
)

const _serviceName = "rides-service"

var gatewayClient = &http.Client{Timeout: 5 * time.Second}

func registerWithGateway(port int) {
	gatewayURL := os.Getenv("GATEWAY_URL")
	secret := os.Getenv("GATEWAY_SECRET")
	if gatewayURL == "" || secret == "" {
		return
	}
	body, _ := json.Marshal(map[string]any{
		"name":         _serviceName,
		"address":      os.Getenv("SERVICE_HOST"),
		"port":         port,
		"health_check": "/health",
	})
	req, err := http.NewRequest(http.MethodPost, gatewayURL+"/internal/register", bytes.NewReader(body))
	if err != nil {
		slog.Error("could not create gateway register request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := gatewayClient.Do(req)
	if err != nil {
		slog.Error("could not register with api-gateway", "error", err)
		return
	}
	resp.Body.Close()
	slog.Info("registered with api-gateway", "service", _serviceName)
}

func deregisterFromGateway() {
	gatewayURL := os.Getenv("GATEWAY_URL")
	secret := os.Getenv("GATEWAY_SECRET")
	if gatewayURL == "" || secret == "" {
		return
	}
	req, err := http.NewRequest(http.MethodDelete, gatewayURL+"/internal/deregister?name="+_serviceName, nil)
	if err != nil {
		slog.Error("could not create gateway deregister request", "error", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := gatewayClient.Do(req)
	if err != nil {
		slog.Error("could not deregister from api-gateway", "error", err)
		return
	}
	resp.Body.Close()
	slog.Info("deregistered from api-gateway", "service", _serviceName)
}

func gracefulShutdown(apiServer *http.Server, reloadCancel context.CancelFunc, kafkaCancel context.CancelFunc, consumer *kafkapkg.Consumer, producer *kafkapkg.Producer, done chan bool) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()

	slog.Info("shutting down gracefully, press Ctrl+C again to force")
	stop()

	reloadCancel()
	kafkaCancel()
	deregisterFromGateway()

	if consumer != nil {
		consumer.Close()
	}
	if producer != nil {
		producer.Close()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := apiServer.Shutdown(ctx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
	}

	slog.Info("server exiting")
	done <- true
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg := config.Load()

	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable&search_path=%s",
		cfg.DBUsername, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBDatabase, cfg.DBSchema)

	// Ensure schema exists before running migrations
	if cfg.DBSchema != "" {
		if err := database.EnsureSchema(connStr, cfg.DBSchema); err != nil {
			slog.Error("failed to ensure schema", "schema", cfg.DBSchema, "error", err)
			os.Exit(1)
		}
	}

	// Run database migrations
	if err := database.RunMigrations(connStr, "migrations"); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}

	db := database.New(connStr)

	// Initialize matching params from DB, falling back to env var defaults
	defaults := config.DefaultMatchingParams()
	mpLoader := config.NewMatchingParamsLoader(defaults)

	// Try to load from DB (ignore error on first run — table may not exist yet)
	ctx := context.Background()
	if raw, err := db.GetMatchingParams(ctx); err == nil && raw != nil && string(raw) != "{}" {
		var p config.MatchingParams
		if err := json.Unmarshal(raw, &p); err == nil && p.MaxPassengers > 0 {
			mpLoader.Store(&p)
		}
	}

	// Start background reload (every 2 minutes)
	reloadCtx, reloadCancel := context.WithCancel(context.Background())
	mpLoader.StartPeriodicReload(reloadCtx, 2*time.Minute, func(ctx context.Context) (*config.MatchingParams, error) {
		raw, err := db.GetMatchingParams(ctx)
		if err != nil || raw == nil {
			return nil, err
		}
		var p config.MatchingParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		return &p, nil
	})

	// Kafka setup
	var kafkaConsumer *kafkapkg.Consumer
	var kafkaProducer *kafkapkg.Producer
	kafkaCtx, kafkaCancel := context.WithCancel(context.Background())

	if cfg.KafkaBootstrapServers != "" {
		kafkaProducer = kafkapkg.NewProducer(cfg.KafkaBootstrapServers)
		locClient := location.NewClient(cfg.LocationServiceURL)

		offerMgr := matching.NewOfferManager(kafkaProducer, db, locClient, mpLoader)
		handler := matching.NewHandler(offerMgr, db, kafkaProducer)

		kafkaConsumer = kafkapkg.NewConsumer(
			cfg.KafkaBootstrapServers,
			"rides-service",
			matching.TopicRideRequests,
			handler.HandleMessage,
		)
		go kafkaConsumer.Start(kafkaCtx)
		slog.Info("kafka consumer started", "topic", matching.TopicRideRequests, "group", "rides-service")
	}

	srv := server.NewServer(cfg, db, mpLoader)

	registerWithGateway(cfg.Port)

	done := make(chan bool, 1)
	go gracefulShutdown(srv, reloadCancel, kafkaCancel, kafkaConsumer, kafkaProducer, done)

	slog.Info("starting rides service", "port", cfg.Port)
	err := srv.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		panic(fmt.Sprintf("http server error: %s", err))
	}

	<-done
	slog.Info("graceful shutdown complete")
}
