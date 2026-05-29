package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"whale2api/internal/auth"
	"whale2api/internal/chathistory"
	"whale2api/internal/config"
	dsclient "whale2api/internal/deepseek/client"
	"whale2api/internal/httpapi/openai/chat"
	"whale2api/internal/httpapi/openai/embeddings"
	"whale2api/internal/httpapi/openai/files"
	"whale2api/internal/httpapi/openai/responses"
	"whale2api/internal/httpapi/openai/shared"
	"whale2api/internal/httpapi/requestbody"
	"whale2api/internal/pooldb"
)

type App struct {
	Store    *config.Store
	PoolDB   *pooldb.DB
	Resolver *auth.Resolver
	DS       *dsclient.Client
	Router   http.Handler
}

// Close releases resources held by the app.
func (a *App) Close() {
	if a != nil && a.PoolDB != nil {
		a.PoolDB.Close()
		a.PoolDB = nil
	}
}

func NewApp() (*App, error) {
	store, err := config.LoadStoreWithError()
	if err != nil {
		return nil, fmt.Errorf("load runtime config: %w", err)
	}
	poolDB, err := pooldb.ConnectFromEnv(context.Background())
	if err != nil {
		return nil, fmt.Errorf("pool database: %w", err)
	}
	if poolDB == nil {
		return nil, fmt.Errorf("WHALE2API_DATABASE_PATH is required (or uses default %s)", pooldb.DefaultDatabasePath)
	}
	var dsClient *dsclient.Client
	resolver := auth.NewResolver(store, func(ctx context.Context, acc config.Account) (string, error) {
		return dsClient.Login(ctx, acc)
	})
	resolver.PoolDB = poolDB
	config.Logger.Info("[pooldb] SQLite gateway pools enabled", "path", pooldb.DatabasePath())
	dsClient = dsclient.NewClient(store, resolver)
	if err := dsClient.PreloadPow(context.Background()); err != nil {
		config.Logger.Warn("[PoW] init failed", "error", err)
	} else {
		config.Logger.Info("[PoW] pure Go solver ready")
	}
	chatHistoryStore := chathistory.New(config.ChatHistoryPath())
	if err := chatHistoryStore.Err(); err != nil {
		config.Logger.Warn("[chat_history] unavailable", "path", chatHistoryStore.Path(), "error", err)
	}

	modelsHandler := &shared.ModelsHandler{Store: store}
	chatHandler := &chat.Handler{Store: store, Auth: resolver, DS: dsClient, ChatHistory: chatHistoryStore}
	responsesHandler := &responses.Handler{Store: store, Auth: resolver, DS: dsClient, ChatHistory: chatHistoryStore}
	filesHandler := &files.Handler{Store: store, Auth: resolver, DS: dsClient, ChatHistory: chatHistoryStore}
	embeddingsHandler := &embeddings.Handler{Store: store, Auth: resolver, DS: dsClient, ChatHistory: chatHistoryStore}
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors)
	r.Use(requestbody.ValidateJSONUTF8)
	r.Use(timeout(0))

	healthzHandler := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
	readyzHandler := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	}
	r.Get("/healthz", healthzHandler)
	r.Head("/healthz", healthzHandler)
	r.Get("/readyz", readyzHandler)
	r.Head("/readyz", readyzHandler)
	r.Get("/v1/models", modelsHandler.ListModels)
	r.Get("/v1/models/{model_id}", modelsHandler.GetModel)
	r.Post("/v1/chat/completions", chatHandler.ChatCompletions)
	r.Post("/v1/responses", responsesHandler.Responses)
	r.Get("/v1/responses/{response_id}", responsesHandler.GetResponseByID)
	r.Post("/v1/files", filesHandler.UploadFile)
	r.Get("/v1/files/{file_id}", filesHandler.RetrieveFile)
	r.Post("/v1/embeddings", embeddingsHandler.Embeddings)
	// Root OpenAI aliases support clients configured with the bare Whale2API service URL.
	r.Get("/models", modelsHandler.ListModels)
	r.Get("/models/{model_id}", modelsHandler.GetModel)
	r.Post("/chat/completions", chatHandler.ChatCompletions)
	r.Post("/responses", responsesHandler.Responses)
	r.Get("/responses/{response_id}", responsesHandler.GetResponseByID)
	r.Post("/files", filesHandler.UploadFile)
	r.Get("/files/{file_id}", filesHandler.RetrieveFile)
	r.Post("/embeddings", embeddingsHandler.Embeddings)
	r.NotFound(http.NotFound)

	return &App{Store: store, PoolDB: poolDB, Resolver: resolver, DS: dsClient, Router: r}, nil
}

func timeout(d time.Duration) func(http.Handler) http.Handler {
	if d <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	return middleware.Timeout(d)
}

var defaultCORSAllowHeaders = []string{
	"Content-Type",
	"Authorization",
	"X-API-Key",
	"X-Whale2-Target-Account",
	"X-Whale2-Source",
}

var blockedCORSRequestHeaders = map[string]struct{}{
	"x-whale2-internal-token": {},
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setCORSHeaders(w, r)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func setCORSHeaders(w http.ResponseWriter, r *http.Request) {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	} else {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		addVaryHeaderToken(w.Header(), "Origin")
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
	w.Header().Set("Access-Control-Allow-Headers", buildCORSAllowHeaders(r))
	w.Header().Set("Access-Control-Max-Age", "600")
	addVaryHeaderToken(w.Header(), "Access-Control-Request-Headers")
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Access-Control-Request-Private-Network")), "true") {
		w.Header().Set("Access-Control-Allow-Private-Network", "true")
		addVaryHeaderToken(w.Header(), "Access-Control-Request-Private-Network")
	}
}

func buildCORSAllowHeaders(r *http.Request) string {
	names := make([]string, 0, len(defaultCORSAllowHeaders)+4)
	seen := make(map[string]struct{}, len(defaultCORSAllowHeaders)+4)
	for _, name := range defaultCORSAllowHeaders {
		appendCORSHeaderName(&names, seen, name)
	}
	if r == nil {
		return strings.Join(names, ", ")
	}
	for _, name := range splitCORSRequestHeaders(r.Header.Get("Access-Control-Request-Headers")) {
		appendCORSHeaderName(&names, seen, name)
	}
	return strings.Join(names, ", ")
}

func splitCORSRequestHeaders(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if !isValidCORSHeaderToken(name) {
			continue
		}
		if _, blocked := blockedCORSRequestHeaders[strings.ToLower(name)]; blocked {
			continue
		}
		out = append(out, name)
	}
	return out
}

func appendCORSHeaderName(dst *[]string, seen map[string]struct{}, name string) {
	name = strings.TrimSpace(name)
	if !isValidCORSHeaderToken(name) {
		return
	}
	key := strings.ToLower(name)
	if _, blocked := blockedCORSRequestHeaders[key]; blocked {
		return
	}
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	*dst = append(*dst, name)
}

func isValidCORSHeaderToken(v string) bool {
	if v == "" {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			continue
		}
		switch c {
		case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
			continue
		default:
			return false
		}
	}
	return true
}

func addVaryHeaderToken(h http.Header, token string) {
	if h == nil {
		return
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	current := h.Values("Vary")
	seen := map[string]struct{}{}
	merged := make([]string, 0, len(current)+1)
	for _, value := range current {
		for _, part := range strings.Split(value, ",") {
			name := strings.TrimSpace(part)
			if name == "" {
				continue
			}
			key := strings.ToLower(name)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, name)
		}
	}
	key := strings.ToLower(token)
	if _, ok := seen[key]; !ok {
		merged = append(merged, token)
	}
	h.Set("Vary", strings.Join(merged, ", "))
}
