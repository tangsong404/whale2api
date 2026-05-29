package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"whale2api/internal/config"
)

func TestCORSPreflightAllowsThirdPartyRequestedHeaders(t *testing.T) {
	handler := cors(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/v1/chat/completions", nil)
	req.Header.Set("Origin", "app://obsidian.md")
	req.Header.Set("Access-Control-Request-Headers", "authorization, x-stainless-os, x-stainless-runtime, x-whale2-internal-token")
	req.Header.Set("Access-Control-Request-Private-Network", "true")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for preflight, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "app://obsidian.md" {
		t.Fatalf("expected origin echo, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Private-Network"); got != "true" {
		t.Fatalf("expected private network allow header, got %q", got)
	}

	allowHeaders := strings.ToLower(rec.Header().Get("Access-Control-Allow-Headers"))
	for _, want := range []string{"authorization", "x-stainless-os", "x-stainless-runtime"} {
		if !strings.Contains(allowHeaders, want) {
			t.Fatalf("expected allow headers to include %q, got %q", want, rec.Header().Get("Access-Control-Allow-Headers"))
		}
	}
	if strings.Contains(allowHeaders, "x-whale2-internal-token") {
		t.Fatalf("expected internal-only header to stay blocked, got %q", rec.Header().Get("Access-Control-Allow-Headers"))
	}

	vary := strings.ToLower(rec.Header().Get("Vary"))
	for _, want := range []string{"origin", "access-control-request-headers", "access-control-request-private-network"} {
		if !strings.Contains(vary, want) {
			t.Fatalf("expected vary to include %q, got %q", want, rec.Header().Get("Vary"))
		}
	}
}

func TestBuildCORSAllowHeadersKeepsDefaultsWithoutRequest(t *testing.T) {
	got := strings.ToLower(buildCORSAllowHeaders(nil))
	for _, want := range []string{"content-type", "authorization", "x-api-key", "x-whale2-source"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected default allow headers to include %q, got %q", want, got)
		}
	}
}

func TestAppCORSPreflightIsUnifiedAcrossInterfaces(t *testing.T) {
	t.Setenv("WHALE2API_ENV_WRITEBACK", "0")
	mem := newTestGatewayPool(t, "k1", []config.Account{{Email: "u@example.com", Password: "p"}})
	app := newTestApp(t, mem)

	cases := []struct {
		name    string
		path    string
		headers string
	}{
		{
			name:    "openai_chat",
			path:    "/v1/chat/completions",
			headers: "authorization, x-stainless-os",
		},
		{
			name:    "openai_responses",
			path:    "/v1/responses",
			headers: "authorization, x-stainless-os",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodOptions, tc.path, nil)
			req.Header.Set("Origin", "app://obsidian.md")
			req.Header.Set("Access-Control-Request-Headers", tc.headers)

			rec := httptest.NewRecorder()
			app.Router.ServeHTTP(rec, req)

			if rec.Code != http.StatusNoContent {
				t.Fatalf("expected %s preflight status 204, got %d", tc.path, rec.Code)
			}
			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "app://obsidian.md" {
				t.Fatalf("expected origin echo for %s, got %q", tc.path, got)
			}
			allowHeaders := strings.ToLower(rec.Header().Get("Access-Control-Allow-Headers"))
			for _, want := range splitCORSRequestHeaders(tc.headers) {
				if !strings.Contains(allowHeaders, strings.ToLower(want)) {
					t.Fatalf("expected allow headers for %s to include %q, got %q", tc.path, want, rec.Header().Get("Access-Control-Allow-Headers"))
				}
			}
		})
	}
}
