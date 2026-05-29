package poolui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"whale2api/internal/config"
	"whale2api/internal/pooldb"
)

type Server struct {
	DB     *pooldb.DB
	Token  string
	Router http.Handler
}

func NewServer(db *pooldb.DB) (*Server, error) {
	token := strings.TrimSpace(os.Getenv("POOL_UI_ADMIN_TOKEN"))
	if token == "" {
		token = "change-me-pool-ui"
		config.Logger.Warn("[poolui] POOL_UI_ADMIN_TOKEN unset; using default (unsafe)")
	}
	s := &Server{DB: db, Token: token}
	if n, err := db.ResetAllRunningAccountTestJobs(context.Background()); err != nil {
		return nil, fmt.Errorf("reset stale account test jobs: %w", err)
	} else if n > 0 {
		config.Logger.Info("[poolui] cleared stale running account test jobs", "count", n)
	}
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(s.cors)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})

	r.Route("/api", func(ar chi.Router) {
		ar.Use(s.requireAuth)
		ar.Get("/keys", s.listKeys)
		ar.Post("/keys", s.createKey)
		ar.Post("/keys/rotate", s.rotateKey)
		ar.Delete("/keys/{api_key}", s.deleteKey)
		ar.Patch("/keys/{api_key}", s.patchKey)
		ar.Get("/keys/{api_key}/accounts", s.listAccounts)
		ar.Get("/keys/{api_key}/export-csv", s.exportCSV)
		ar.Post("/keys/{api_key}/accounts", s.addAccount)
		ar.Post("/keys/{api_key}/import-csv", s.importCSV)
		ar.Get("/keys/{api_key}/accounts/test", s.getAccountTestJob)
		ar.Post("/keys/{api_key}/accounts/test/cancel", s.cancelAccountTestJob)
		ar.Post("/keys/{api_key}/accounts/test", s.testAccounts)
		ar.Post("/keys/{api_key}/accounts/{identifier}/test", s.testOneAccount)
		ar.Post("/keys/{api_key}/accounts/{identifier}/discard", s.discardAccount)
		ar.Post("/keys/{api_key}/accounts/{identifier}/restore", s.restoreAccount)
	})

	r.NotFound(s.serveStatic)

	s.Router = r
	return s, nil
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.checkAuth(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"detail": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) checkAuth(r *http.Request) bool {
	tok := extractBearer(r)
	if tok == "" {
		tok = strings.TrimSpace(r.Header.Get("x-pool-ui-token"))
	}
	return tok != "" && tok == s.Token
}

func extractBearer(r *http.Request) string {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Pool-UI-Token")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) listKeys(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.ListGatewayKeys(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": rows})
}

func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APIKey string `json:"api_key"`
		Name   string `json:"name"`
		Remark string `json:"remark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid json"})
		return
	}
	apiKey, err := pooldb.NormalizeOrGenerateGatewayAPIKey(req.APIKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": err.Error()})
		return
	}
	if err := s.DB.CreateGatewayKey(r.Context(), apiKey, req.Name, req.Remark); err != nil {
		if pooldb.IsUniqueViolation(err) {
			writeJSON(w, http.StatusConflict, map[string]any{"detail": err.Error()})
			return
		}
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "api_key": apiKey})
}

func (s *Server) rotateKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OldAPIKey string `json:"old_api_key"`
		NewAPIKey string `json:"new_api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid json"})
		return
	}
	newKey, err := pooldb.NormalizeOrGenerateGatewayAPIKey(req.NewAPIKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": err.Error()})
		return
	}
	if err := s.DB.RotateGatewayAPIKey(r.Context(), req.OldAPIKey, newKey); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "api_key": newKey})
}

func (s *Server) patchKey(w http.ResponseWriter, r *http.Request) {
	key := routeAPIKey(r)
	var req struct {
		Name    string `json:"name"`
		Remark  string `json:"remark"`
		Enabled *bool  `json:"enabled"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := s.DB.UpdateGatewayKeyMeta(r.Context(), key, req.Name, req.Remark, req.Enabled); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	key := routeAPIKey(r)
	include := strings.TrimSpace(r.URL.Query().Get("include_discarded")) == "1" ||
		strings.EqualFold(r.URL.Query().Get("include_discarded"), "true")
	rows, err := s.DB.ListPoolAccountsAll(r.Context(), key, include)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"api_key": key, "accounts": rows})
}

func (s *Server) addAccount(w http.ResponseWriter, r *http.Request) {
	key := routeAPIKey(r)
	var req struct {
		Email      string `json:"email"`
		Identifier string `json:"identifier"`
		Password   string `json:"password"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	ident := strings.TrimSpace(req.Identifier)
	if ident == "" {
		ident = strings.TrimSpace(req.Email)
	}
	if err := s.DB.AddAccountToPool(r.Context(), key, ident, req.Password); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) deleteKey(w http.ResponseWriter, r *http.Request) {
	key := routeAPIKey(r)
	if err := s.DB.DeleteGatewayKey(r.Context(), key); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) exportCSV(w http.ResponseWriter, r *http.Request) {
	key := routeAPIKey(r)
	includeDiscarded := true
	if strings.TrimSpace(r.URL.Query().Get("active_only")) == "1" ||
		strings.EqualFold(r.URL.Query().Get("active_only"), "true") {
		includeDiscarded = false
	}
	body, err := s.DB.ExportAccountsCSV(r.Context(), key, includeDiscarded)
	if err != nil {
		writeErr(w, err)
		return
	}
	filename := "pool-accounts.csv"
	if key != "" {
		safe := strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
				return r
			}
			return '_'
		}, key)
		if len(safe) > 48 {
			safe = safe[:48]
		}
		filename = safe + ".csv"
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *Server) importCSV(w http.ResponseWriter, r *http.Request) {
	key := routeAPIKey(r)
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "read body failed"})
		return
	}
	res, err := s.DB.ImportAccountsCSV(r.Context(), key, bytes.NewReader(body))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) discardAccount(w http.ResponseWriter, r *http.Request) {
	key := routeAPIKey(r)
	ident := routeIdentifier(r)
	if err := s.DB.SetAccountDiscarded(r.Context(), key, ident, true); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "discarded": true})
}

func (s *Server) restoreAccount(w http.ResponseWriter, r *http.Request) {
	key := routeAPIKey(r)
	ident := routeIdentifier(r)
	if err := s.DB.SetAccountDiscarded(r.Context(), key, ident, false); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "discarded": false})
}

func writeErr(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
