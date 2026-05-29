package auth

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"whale2api/internal/config"
)

func TestDetermineRejectsUnknownXAPIKey(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)
	req.Header.Set("x-api-key", "not-in-config-keys")

	_, err := r.Determine(req)
	if !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("expected ErrInvalidAPIKey, got %v", err)
	}
}

func TestDetermineWithXAPIKeyManagedKeyAcquiresAccount(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)
	req.Header.Set("x-api-key", "managed-key")

	auth, err := r.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	defer r.Release(auth)
	if !auth.UseConfigToken {
		t.Fatalf("expected managed key mode")
	}
	if auth.AccountID != "acc@example.com" {
		t.Fatalf("unexpected account id: %q", auth.AccountID)
	}
	if auth.DeepSeekToken != "account-token" {
		t.Fatalf("unexpected account token: %q", auth.DeepSeekToken)
	}
	if auth.CallerID == "" {
		t.Fatalf("expected caller id to be populated")
	}
}

func TestDetermineCallerWithManagedKeySkipsAccountAcquire(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodGet, "/v1/responses/resp_1", nil)
	req.Header.Set("x-api-key", "managed-key")

	a, err := r.DetermineCaller(req)
	if err != nil {
		t.Fatalf("determine caller failed: %v", err)
	}
	if a.CallerID == "" {
		t.Fatalf("expected caller id to be populated")
	}
	if a.UseConfigToken {
		t.Fatalf("expected no config-token lease for caller-only auth")
	}
	if a.AccountID != "" {
		t.Fatalf("expected empty account id, got %q", a.AccountID)
	}
}

func TestCallerTokenIDStable(t *testing.T) {
	a := callerTokenID("token-a")
	b := callerTokenID("token-a")
	c := callerTokenID("token-b")
	if a == "" || b == "" || c == "" {
		t.Fatalf("expected non-empty caller ids")
	}
	if a != b {
		t.Fatalf("expected stable caller id, got %q and %q", a, b)
	}
	if a == c {
		t.Fatalf("expected different caller id for different tokens")
	}
}

func TestDetermineMissingToken(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	_, err := r.Determine(req)
	if err == nil {
		t.Fatal("expected unauthorized error")
	}
	if err != ErrUnauthorized {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDetermineRejectsUnknownQueryKey(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent?key=unknown-query-key", nil)

	_, err := r.Determine(req)
	if !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("expected ErrInvalidAPIKey, got %v", err)
	}
}

func TestDetermineRejectsUnknownXGoogAPIKey(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:streamGenerateContent?alt=sse", nil)
	req.Header.Set("x-goog-api-key", "unknown-goog-key")

	_, err := r.Determine(req)
	if !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("expected ErrInvalidAPIKey, got %v", err)
	}
}

func TestDetermineRejectsUnknownAPIKeyQueryParam(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent?api_key=unknown-api-key", nil)

	_, err := r.Determine(req)
	if !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("expected ErrInvalidAPIKey, got %v", err)
	}
}

func TestDetermineManagedKeyViaQueryKey(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent?key=managed-key", nil)

	a, err := r.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	defer r.Release(a)
	if !a.UseConfigToken {
		t.Fatalf("expected managed key mode")
	}
	if a.AccountID != "acc@example.com" {
		t.Fatalf("unexpected account id: %q", a.AccountID)
	}
}

func TestDetermineManagedKeyViaXGoogAPIKey(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:streamGenerateContent?alt=sse", nil)
	req.Header.Set("x-goog-api-key", "managed-key")

	a, err := r.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	defer r.Release(a)
	if !a.UseConfigToken {
		t.Fatalf("expected managed key mode")
	}
}

func TestDetermineHeaderTokenPrecedenceOverQueryKey(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent?key=query-key", nil)
	req.Header.Set("x-api-key", "managed-key")

	a, err := r.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	defer r.Release(a)
	if !a.UseConfigToken {
		t.Fatalf("expected managed key mode from header token")
	}
	if a.AccountID == "" {
		t.Fatalf("expected managed account to be acquired")
	}
}

func TestDetermineCallerMissingToken(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodGet, "/v1/responses/resp_1", nil)

	_, err := r.DetermineCaller(req)
	if err == nil {
		t.Fatal("expected unauthorized error")
	}
	if err != ErrUnauthorized {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDetermineCallerRejectsUnknownKey(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodGet, "/v1/responses/resp_1", nil)
	req.Header.Set("x-api-key", "not-registered")

	_, err := r.DetermineCaller(req)
	if !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("expected ErrInvalidAPIKey, got %v", err)
	}
}

func TestDetermineManagedAccountForcesRefreshEverySixHours(t *testing.T) {
	var loginCount int32
	resolver := newTestResolverWithAccounts(t, "managed-key", []config.Account{
		{Email: "acc@example.com", Password: "pwd", Token: "seed-token"},
	}, func(_ context.Context, _ config.Account) (string, error) {
		n := atomic.AddInt32(&loginCount, 1)
		return "fresh-token-" + string(rune('0'+n)), nil
	})

	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("x-api-key", "managed-key")

	a1, err := resolver.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	if a1.DeepSeekToken != "seed-token" {
		t.Fatalf("expected initial token without forced refresh, got %q", a1.DeepSeekToken)
	}
	resolver.Release(a1)
	if got := atomic.LoadInt32(&loginCount); got != 0 {
		t.Fatalf("expected no login before refresh interval, got %d", got)
	}

	resolver.mu.Lock()
	resolver.tokenRefreshedAt["acc@example.com"] = time.Now().Add(-7 * time.Hour)
	resolver.mu.Unlock()

	a2, err := resolver.Determine(req)
	if err != nil {
		t.Fatalf("determine after interval failed: %v", err)
	}
	defer resolver.Release(a2)
	if a2.DeepSeekToken != "fresh-token-1" {
		t.Fatalf("expected refreshed token after interval, got %q", a2.DeepSeekToken)
	}
	if got := atomic.LoadInt32(&loginCount); got != 1 {
		t.Fatalf("expected exactly one forced refresh login, got %d", got)
	}
}

func TestDetermineManagedAccountUsesUpdatedRefreshInterval(t *testing.T) {
	var loginCount int32
	resolver := newTestResolverWithAccounts(t, "managed-key", []config.Account{
		{Email: "acc@example.com", Password: "pwd", Token: "seed-token"},
	}, func(_ context.Context, _ config.Account) (string, error) {
		n := atomic.AddInt32(&loginCount, 1)
		return "fresh-token-" + string(rune('0'+n)), nil
	})
	if err := resolver.Store.Update(func(c *config.Config) error {
		c.Runtime.TokenRefreshIntervalHours = 6
		return nil
	}); err != nil {
		t.Fatalf("seed runtime failed: %v", err)
	}

	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("x-api-key", "managed-key")

	a1, err := resolver.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	if a1.DeepSeekToken != "seed-token" {
		t.Fatalf("expected initial token without forced refresh, got %q", a1.DeepSeekToken)
	}
	resolver.Release(a1)
	if got := atomic.LoadInt32(&loginCount); got != 0 {
		t.Fatalf("expected no login before runtime update, got %d", got)
	}

	if err := resolver.Store.Update(func(c *config.Config) error {
		c.Runtime.TokenRefreshIntervalHours = 1
		return nil
	}); err != nil {
		t.Fatalf("update runtime failed: %v", err)
	}

	resolver.mu.Lock()
	resolver.tokenRefreshedAt["acc@example.com"] = time.Now().Add(-2 * time.Hour)
	resolver.mu.Unlock()

	a2, err := resolver.Determine(req)
	if err != nil {
		t.Fatalf("determine after runtime update failed: %v", err)
	}
	defer resolver.Release(a2)
	if a2.DeepSeekToken != "fresh-token-1" {
		t.Fatalf("expected refreshed token after runtime update, got %q", a2.DeepSeekToken)
	}
	if got := atomic.LoadInt32(&loginCount); got != 1 {
		t.Fatalf("expected exactly one login after runtime update, got %d", got)
	}
}

func TestDetermineManagedAccountRetriesOtherAccountOnLoginFailure(t *testing.T) {
	resolver := newTestResolverWithAccounts(t, "managed-key", []config.Account{
		{Email: "bad@example.com", Password: "pwd"},
		{Email: "good@example.com", Password: "pwd"},
	}, func(_ context.Context, acc config.Account) (string, error) {
		if acc.Email == "bad@example.com" {
			return "", errors.New("stale account")
		}
		return "fresh-good-token", nil
	})

	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("x-api-key", "managed-key")

	a, err := resolver.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	defer resolver.Release(a)
	if a.AccountID != "good@example.com" {
		t.Fatalf("expected fallback to good account, got %q", a.AccountID)
	}
	if a.DeepSeekToken == "" {
		t.Fatal("expected non-empty token from fallback account")
	}
	if !a.TriedAccounts["bad@example.com"] {
		t.Fatalf("expected bad account to be tracked as tried")
	}
}

func TestDetermineTargetAccountDoesNotFallbackOnLoginFailure(t *testing.T) {
	resolver := newTestResolverWithAccounts(t, "managed-key", []config.Account{
		{Email: "bad@example.com", Password: "pwd"},
		{Email: "good@example.com", Password: "pwd", Token: "good-token"},
	}, func(_ context.Context, acc config.Account) (string, error) {
		if acc.Email == "bad@example.com" {
			return "", errors.New("stale account")
		}
		return "fresh-good-token", nil
	})

	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("x-api-key", "managed-key")
	req.Header.Set("X-Whale2-Target-Account", "bad@example.com")

	_, err := resolver.Determine(req)
	if err == nil {
		t.Fatal("expected determine to fail for broken target account")
	}
}

func TestDetermineManagedAccountReturnsLastEnsureErrorWhenAllFail(t *testing.T) {
	ensureErr := errors.New("all credentials stale")
	resolver := newTestResolverWithAccounts(t, "managed-key", []config.Account{
		{Email: "bad1@example.com", Password: "pwd"},
		{Email: "bad2@example.com", Password: "pwd"},
	}, func(_ context.Context, _ config.Account) (string, error) {
		return "", ensureErr
	})

	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("x-api-key", "managed-key")

	_, err := resolver.Determine(req)
	if err == nil {
		t.Fatal("expected determine to fail")
	}
	if !errors.Is(err, ensureErr) {
		t.Fatalf("expected ensure error, got %v", err)
	}
	if errors.Is(err, ErrNoAccount) {
		t.Fatalf("expected auth-style ensure error, got ErrNoAccount")
	}
}

func TestDetermineRejectsUnknownAPIKey(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer not-a-configured-key")

	_, err := r.Determine(req)
	if err == nil {
		t.Fatal("expected error for unknown api key")
	}
	if !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("expected ErrInvalidAPIKey, got %v", err)
	}
}
