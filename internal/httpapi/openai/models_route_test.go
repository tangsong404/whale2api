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
		req := httptest.NewRequest(http.MethodGet, "/v1/models/deepseek-flash", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("retired_vision_exp_rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/models/deepseek-v4-flash-vision-exp", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
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
		req := httptest.NewRequest(http.MethodGet, "/v1/models/deepseek-flash-nothinking", nil)
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
	if !ok || len(data) != 1 {
		t.Fatalf("expected exactly 1 model, got %#v", body["data"])
	}
	m, ok := data[0].(map[string]any)
	if !ok {
		t.Fatalf("expected object in data, got %T", data[0])
	}
	if id, _ := m["id"].(string); id != "deepseek-flash" {
		t.Fatalf("expected deepseek-flash, got %v", m["id"])
	}
	cl, ok := m["context_length"].(float64)
	if !ok || int(cl) != config.AdvertisedMaxContextTokens {
		t.Fatalf("expected context_length=%d, got %v", config.AdvertisedMaxContextTokens, m["context_length"])
	}
	multimodal, ok := m["multimodal"].(bool)
	if !ok || !multimodal {
		t.Fatalf("expected multimodal=true, got %v", m["multimodal"])
	}
}

func TestGetModelByIDIncludesContextLength(t *testing.T) {
	h := &openAITestSurface{}
	r := chi.NewRouter()
	registerOpenAITestRoutes(r, h)

	req := httptest.NewRequest(http.MethodGet, "/v1/models/deepseek-flash", nil)
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
	multimodal, ok := body["multimodal"].(bool)
	if !ok || !multimodal {
		t.Fatalf("expected multimodal=true for deepseek-flash, got %v", body["multimodal"])
	}
}

func TestGetModelByIDRejectsRetiredIDs(t *testing.T) {
	h := &openAITestSurface{}
	r := chi.NewRouter()
	registerOpenAITestRoutes(r, h)

	for _, retired := range []string{"deepseek-v4-flash", "deepseek-v4-flash-vision-exp"} {
		req := httptest.NewRequest(http.MethodGet, "/v1/models/"+retired, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: expected 404, got %d body=%s", retired, rec.Code, rec.Body.String())
		}
	}
}
