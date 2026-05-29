package poolui

import (
	"net/http"
	"strings"

	"whale2api/internal/pooldb"
)

type accountTestRequest struct {
	Identifiers []string `json:"identifiers"`
	Identifier  string   `json:"identifier"`
	ActiveOnly  *bool    `json:"active_only"`
}

func (s *Server) testAccounts(w http.ResponseWriter, r *http.Request) {
	s.startAccountTestJob(w, r)
}

func (s *Server) testOneAccount(w http.ResponseWriter, r *http.Request) {
	apiKey := routeAPIKey(r)
	ident := routeIdentifier(r)
	if ident == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "identifier is required"})
		return
	}
	creds, err := s.DB.ListPoolAccountCredentials(r.Context(), apiKey, []string{ident}, false)
	if err != nil {
		writeErr(w, err)
		return
	}
	if len(creds) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "account not in pool"})
		return
	}
	s.beginAccountTest(w, r, apiKey, creds)
}

func (s *Server) beginAccountTest(w http.ResponseWriter, r *http.Request, apiKey string, creds []pooldb.PoolAccountCredential) {
	if len(creds) == 0 {
		writeJSON(w, http.StatusOK, accountTestResponse{Results: []accountTestResult{}})
		return
	}
	if err := s.DB.StartAccountTestJob(r.Context(), apiKey, len(creds)); err != nil {
		writeErr(w, err)
		return
	}
	job, _ := s.DB.GetAccountTestJob(r.Context(), apiKey)
	writeJSON(w, http.StatusAccepted, jobToResponse(job))
	go s.runPersistedAccountTests(apiKey, creds)
}

func (req accountTestRequest) identifiers() []string {
	var out []string
	seen := map[string]struct{}{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	add(req.Identifier)
	for _, id := range req.Identifiers {
		add(id)
	}
	return out
}
