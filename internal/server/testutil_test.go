package server

import (
	"context"
	"net/http"
	"testing"

	"whale2api/internal/auth"
	"whale2api/internal/chathistory"
	"whale2api/internal/config"
	"whale2api/internal/pooldb"
	dsclient "whale2api/internal/deepseek/client"
	"whale2api/internal/httpapi/openai/chat"
	"whale2api/internal/httpapi/openai/embeddings"
	"whale2api/internal/httpapi/openai/files"
	"whale2api/internal/httpapi/openai/responses"
	"whale2api/internal/httpapi/openai/shared"
	"whale2api/internal/httpapi/requestbody"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func newTestGatewayPool(t *testing.T, apiKey string, accounts []config.Account) *pooldb.Mem {
	t.Helper()
	mem := pooldb.NewMem()
	mem.RegisterKey(apiKey, accounts, true)
	return mem
}

func newTestApp(t *testing.T, pool auth.GatewayPool) *App {
	t.Helper()
	store, err := config.LoadStoreWithError()
	if err != nil {
		t.Fatalf("LoadStoreWithError: %v", err)
	}
	var dsClient *dsclient.Client
	resolver := auth.NewResolver(store, func(ctx context.Context, acc config.Account) (string, error) {
		return dsClient.Login(ctx, acc)
	})
	resolver.PoolDB = pool
	dsClient = dsclient.NewClient(store, resolver)
	chatHistoryStore := chathistory.New(config.ChatHistoryPath())

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
	r.Get("/models", modelsHandler.ListModels)
	r.Get("/models/{model_id}", modelsHandler.GetModel)
	r.Post("/chat/completions", chatHandler.ChatCompletions)
	r.Post("/responses", responsesHandler.Responses)
	r.Get("/responses/{response_id}", responsesHandler.GetResponseByID)
	r.Post("/files", filesHandler.UploadFile)
	r.Get("/files/{file_id}", filesHandler.RetrieveFile)
	r.Post("/embeddings", embeddingsHandler.Embeddings)
	r.NotFound(http.NotFound)

	return &App{Store: store, Resolver: resolver, DS: dsClient, Router: r}
}
