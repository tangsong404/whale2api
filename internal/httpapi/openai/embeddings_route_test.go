package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"whale2api/internal/auth"
	"whale2api/internal/config"
	"whale2api/internal/pooldb"
)

func newEmbeddingsTestResolver(t *testing.T, apiKey string, embeddingsProvider string) (*config.Store, *auth.Resolver) {
	t.Helper()
	store := config.LoadStore()
	if embeddingsProvider != "" {
		if err := store.Update(func(c *config.Config) error {
			c.Embeddings.Provider = embeddingsProvider
			return nil
		}); err != nil {
			t.Fatalf("update embeddings config: %v", err)
		}
	}
	mem := pooldb.NewMem()
	mem.RegisterKey(apiKey, []config.Account{{Email: "u@example.com", Token: "acc-tok"}}, true)
	resolver := auth.NewResolver(store, func(_ context.Context, _ config.Account) (string, error) {
		return "unused", nil
	})
	resolver.PoolDB = mem
	return store, resolver
}

func TestEmbeddingsRouteContract(t *testing.T) {
	store, resolver := newEmbeddingsTestResolver(t, "test-token", "deterministic")
	h := &openAITestSurface{Store: store, Auth: resolver}
	r := chi.NewRouter()
	registerOpenAITestRoutes(r, h)

	t.Run("unauthorized", func(t *testing.T) {
		body := bytes.NewBufferString(`{"model":"deepseek-v4-flash","input":"hello"}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", body)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("ok", func(t *testing.T) {
		body := bytes.NewBufferString(`{"model":"deepseek-v4-flash","input":["a","b"]}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", body)
		req.Header.Set("Authorization", "Bearer test-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode response failed: %v", err)
		}
		if out["object"] != "list" {
			t.Fatalf("unexpected object: %#v", out["object"])
		}
		data, _ := out["data"].([]any)
		if len(data) != 2 {
			t.Fatalf("expected 2 embeddings, got %d", len(data))
		}
	})
}

func TestEmbeddingsRouteProviderMissing(t *testing.T) {
	store, resolver := newEmbeddingsTestResolver(t, "test-token", "")
	h := &openAITestSurface{Store: store, Auth: resolver}
	r := chi.NewRouter()
	registerOpenAITestRoutes(r, h)

	body := bytes.NewBufferString(`{"model":"deepseek-v4-flash","input":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", body)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	errObj, _ := out["error"].(map[string]any)
	if _, ok := errObj["code"]; !ok {
		t.Fatalf("expected error.code in response: %#v", out)
	}
	if _, ok := errObj["param"]; !ok {
		t.Fatalf("expected error.param in response: %#v", out)
	}
}
