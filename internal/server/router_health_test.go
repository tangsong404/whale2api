package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"whale2api/internal/config"
)

func TestHealthEndpointsSupportHEAD(t *testing.T) {
	t.Setenv("WHALE2API_ENV_WRITEBACK", "0")
	mem := newTestGatewayPool(t, "k1", []config.Account{{Email: "u@example.com", Password: "p"}})
	app := newTestApp(t, mem)

	for _, path := range []string{"/healthz", "/readyz"} {
		req := httptest.NewRequest(http.MethodHead, path, nil)
		rec := httptest.NewRecorder()
		app.Router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected %s HEAD status 200, got %d", path, rec.Code)
		}
	}
}
