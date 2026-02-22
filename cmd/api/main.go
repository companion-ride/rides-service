package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"rides/internal/config"
	"rides/internal/database"
	"rides/internal/server"
)

const _serviceName = "rides-service"

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
	req, _ := http.NewRequest(http.MethodPost, gatewayURL+"/internal/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		log.Printf("could not register with api-gateway: %v", err)
		return
	}
	resp.Body.Close()
	log.Printf("registered with api-gateway as %s", _serviceName)
}

func deregisterFromGateway() {
	gatewayURL := os.Getenv("GATEWAY_URL")
	secret := os.Getenv("GATEWAY_SECRET")
	if gatewayURL == "" || secret == "" {
		return
	}
	req, _ := http.NewRequest(http.MethodDelete, gatewayURL+"/internal/deregister?name="+_serviceName, nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		log.Printf("could not deregister from api-gateway: %v", err)
		return
	}
	resp.Body.Close()
	log.Printf("deregistered from api-gateway: %s", _serviceName)
}

// ResolveService returns "host:port" for a service registered in the gateway.
// Returns empty string if GATEWAY_URL is not configured or service is not found.
func ResolveService(name string) string {
	gatewayURL := os.Getenv("GATEWAY_URL")
	secret := os.Getenv("GATEWAY_SECRET")
	if gatewayURL == "" || secret == "" {
		return ""
	}
	req, _ := http.NewRequest(http.MethodGet, gatewayURL+"/internal/resolve?name="+name, nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return ""
	}
	defer resp.Body.Close()
	var result struct {
		Address string `json:"address"`
		Port    int    `json:"port"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ""
	}
	return fmt.Sprintf("http://%s:%d", result.Address, result.Port)
}

func gracefulShutdown(apiServer *http.Server, done chan bool) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()

	log.Println("shutting down gracefully, press Ctrl+C again to force")
	stop()

	deregisterFromGateway()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := apiServer.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown with error: %v", err)
	}

	log.Println("Server exiting")
	done <- true
}

func main() {
	cfg := config.Load()

	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable&search_path=%s",
		cfg.DBUsername, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBDatabase, cfg.DBSchema)

	// Run database migrations
	if err := database.RunMigrations(connStr, "migrations"); err != nil {
		log.Printf("migration warning: %v", err)
	}

	db := database.New(connStr)

	srv := server.NewServer(cfg, db)

	registerWithGateway(cfg.Port)

	done := make(chan bool, 1)
	go gracefulShutdown(srv, done)

	log.Printf("Starting rides service on port %d", cfg.Port)
	err := srv.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		panic(fmt.Sprintf("http server error: %s", err))
	}

	<-done
	log.Println("Graceful shutdown complete.")
}
