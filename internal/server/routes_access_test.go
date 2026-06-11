package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rides/internal/config"
)

// accessTestServer builds the real router from the repo root so ./web resolves.
func accessTestServer(t *testing.T) http.Handler {
	t.Chdir("../..")
	s := &Server{cfg: &config.Config{JWTSecretKey: "test", DoubleGISAPIKey: "map-key"}}
	return s.RegisterRoutes()
}

func get(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(method, path, nil))
	return rr
}

func TestDebugRoutesDisabledByDefault(t *testing.T) {
	t.Setenv("ENABLE_DEBUG_PAGES", "")
	h := accessTestServer(t)
	for _, path := range []string{"/config", "/driver", "/passenger"} {
		rr := get(h, http.MethodGet, path)
		if rr.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want %d", path, rr.Code, http.StatusNotFound)
		}
		if strings.Contains(rr.Body.String(), "map-key") {
			t.Errorf("GET %s leaked the map key", path)
		}
	}
}

func TestDebugRoutesEnabled(t *testing.T) {
	t.Setenv("ENABLE_DEBUG_PAGES", "true")
	h := accessTestServer(t)
	for _, path := range []string{"/driver", "/passenger"} {
		if rr := get(h, http.MethodGet, path); rr.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want %d", path, rr.Code, http.StatusOK)
		}
	}
	rr := get(h, http.MethodGet, "/config")
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "map-key") {
		t.Errorf("GET /config = %d %q, want 200 with map key", rr.Code, rr.Body.String())
	}
}

func TestDeleteAllRidesRequiresAuth(t *testing.T) {
	h := accessTestServer(t)
	if rr := get(h, http.MethodDelete, "/admin/rides"); rr.Code != http.StatusUnauthorized {
		t.Errorf("DELETE /admin/rides without token = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
	if rr := get(h, http.MethodDelete, "/admin/rides/all"); rr.Code != http.StatusNotFound {
		t.Errorf("DELETE /admin/rides/all = %d, want %d", rr.Code, http.StatusNotFound)
	}
}
