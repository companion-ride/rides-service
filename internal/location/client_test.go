package location

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetCurrentLocationSendsInternalKey(t *testing.T) {
	var gotKey, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Internal-Key")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"user_id":"u1","lat":43.2,"lon":76.9}`))
	}))
	defer srv.Close()

	loc, err := NewClient(srv.URL, "k1").GetCurrentLocation(context.Background(), "u1")
	if err != nil {
		t.Fatalf("GetCurrentLocation: %v", err)
	}
	if gotKey != "k1" {
		t.Errorf("X-Internal-Key = %q, want %q", gotKey, "k1")
	}
	if gotPath != "/last/u1" {
		t.Errorf("path = %q, want %q", gotPath, "/last/u1")
	}
	if loc.UserID != "u1" || loc.Lat != 43.2 || loc.Lon != 76.9 {
		t.Errorf("location = %+v", loc)
	}
}

func TestGetCurrentLocationRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	if _, err := NewClient(srv.URL, "").GetCurrentLocation(context.Background(), "u1"); err == nil {
		t.Fatal("expected error on 401, got nil")
	}
}
