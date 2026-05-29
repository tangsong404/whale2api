package poolui

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
)

func routeAPIKey(r *http.Request) string {
	raw := strings.TrimSpace(chi.URLParam(r, "api_key"))
	if raw == "" {
		return ""
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return raw
	}
	return strings.TrimSpace(decoded)
}

func routeIdentifier(r *http.Request) string {
	raw := strings.TrimSpace(chi.URLParam(r, "identifier"))
	if raw == "" {
		return ""
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return raw
	}
	return strings.TrimSpace(decoded)
}
