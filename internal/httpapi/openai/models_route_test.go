package openai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"whale2api/internal/config"
)

func TestGetModelRouteDirectAndAlias(t *testing.T) {
	h := &openAITestSurface{}
	r := chi.NewRouter()
	registerOpenAITestRoutes(r, h)

	t.Run("flash", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/models/deepseek-v4-flash", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("pro_rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/models/deepseek-v4-pro", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("nothinking_rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/models/deepseek-v4-flash-nothinking", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("unknown_model", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/models/gpt-4.1", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for unknown model, got %d body=%s", rec.Code, rec.Body.String())
		}
	})
}

func TestGetModelRouteNotFound(t *testing.T) {
	h := &openAITestSurface{}
	r := chi.NewRouter()
	registerOpenAITestRoutes(r, h)

	req := httptest.NewRequest(http.MethodGet, "/v1/models/not-exists", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestListModelsIncludesContextLength(t *testing.T) {
	h := &openAITestSurface{}
	r := chi.NewRouter()
	registerOpenAITestRoutes(r, h)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	data, ok := body["data"].([]any)
	if !ok || len(data) == 0 {
		t.Fatalf("expected data array, got %#v", body["data"])
	}
	for _, it := range data {
		m, ok := it.(map[string]any)
		if !ok {
			t.Fatalf("expected object in data, got %T", it)
		}
		id, _ := m["id"].(string)
		cl, ok := m["context_length"].(float64)
		if !ok || int(cl) != config.AdvertisedMaxContextTokens {
			t.Fatalf("model %q: expected context_length=%d, got %v", id, config.AdvertisedMaxContextTokens, m["context_length"])
		}
	}
}

func TestGetModelByIDIncludesContextLength(t *testing.T) {
	h := &openAITestSurface{}
	r := chi.NewRouter()
	registerOpenAITestRoutes(r, h)

	req := httptest.NewRequest(http.MethodGet, "/v1/models/deepseek-v4-flash", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	cl, ok := body["context_length"].(float64)
	if !ok || int(cl) != config.AdvertisedMaxContextTokens {
		t.Fatalf("expected context_length=%d, got %v", config.AdvertisedMaxContextTokens, body["context_length"])
	}
}
