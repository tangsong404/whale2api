package responses

import (
	"context"
	"strings"
	"time"

	"whale2api/internal/auth"
	"whale2api/internal/config"
)

func (h *Handler) autoDeleteRemoteSession(ctx context.Context, a *auth.RequestAuth, sessionID string) {
	if a == nil || h == nil || h.DS == nil || a.DeepSeekToken == "" {
		return
	}
	deleteBaseCtx := context.WithoutCancel(ctx)
	deleteCtx, cancel := context.WithTimeout(deleteBaseCtx, 10*time.Second)
	defer cancel()
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	_, err := h.DS.DeleteSessionForToken(deleteCtx, a.DeepSeekToken, sessionID)
	if err != nil {
		config.Logger.Warn("[responses_auto_delete_sessions] single delete failed", "account", a.AccountID, "session_id", sessionID, "error", err)
		return
	}
	config.Logger.Debug("[responses_auto_delete_sessions] single delete success", "account", a.AccountID, "session_id", sessionID)
}
