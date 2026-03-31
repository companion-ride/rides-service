package configclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestFetch_ReturnsConfigOnSuccess(t *testing.T) {
	cfg := map[string]any{"maxPassengers": 3}
	cfgJSON, _ := json.Marshal(cfg)
	checksum := "abc123"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/service/rides" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer testkey" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("ETag", `"`+checksum+`"`)
		json.NewEncoder(w).Encode(map[string]any{
			"config":   cfg,
			"checksum": checksum,
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "testkey", "rides")
	raw, changed, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true on first fetch")
	}
	if string(raw) != string(cfgJSON) {
		t.Errorf("config mismatch: got %s, want %s", raw, cfgJSON)
	}
}

func TestFetch_Returns304NotChanged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != "" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"abc"`)
		json.NewEncoder(w).Encode(map[string]any{
			"config":   map[string]any{"a": 1},
			"checksum": "abc",
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "key", "test")
	// First fetch to set ETag
	_, _, _ = c.Fetch(context.Background())
	// Second fetch should return not changed
	_, changed, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Fatal("expected changed=false on 304")
	}
}

func TestFetch_ReturnsErrorOnServerFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "key", "test")
	_, _, err := c.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestStartPolling_CallsOnUpdate(t *testing.T) {
	var reqCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := reqCount.Add(1)
		w.Header().Set("ETag", `"v`+string(rune('0'+n))+`"`)
		json.NewEncoder(w).Encode(map[string]any{
			"config":   map[string]any{"n": n},
			"checksum": "v" + string(rune('0'+n)),
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "key", "test")
	var updates atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c.StartPolling(ctx, 50*time.Millisecond, func(raw json.RawMessage) {
		updates.Add(1)
	})

	time.Sleep(200 * time.Millisecond)
	cancel()

	if updates.Load() < 2 {
		t.Errorf("expected at least 2 updates, got %d", updates.Load())
	}
}
