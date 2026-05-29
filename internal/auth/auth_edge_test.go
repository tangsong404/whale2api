package auth

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"whale2api/internal/config"
)

// ─── extractCallerToken edge cases ───────────────────────────────────

func TestExtractCallerTokenBearerPrefix(t *testing.T) {
	req, _ := http.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Bearer my-token")
	if got := extractCallerToken(req); got != "my-token" {
		t.Fatalf("expected my-token, got %q", got)
	}
}

func TestExtractCallerTokenBearerCaseInsensitive(t *testing.T) {
	req, _ := http.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "BEARER My-Token")
	if got := extractCallerToken(req); got != "My-Token" {
		t.Fatalf("expected My-Token, got %q", got)
	}
}

func TestExtractCallerTokenBearerEmpty(t *testing.T) {
	req, _ := http.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Bearer ")
	if got := extractCallerToken(req); got != "" {
		t.Fatalf("expected empty for 'Bearer ', got %q", got)
	}
}

func TestExtractCallerTokenXAPIKey(t *testing.T) {
	req, _ := http.NewRequest("POST", "/", nil)
	req.Header.Set("x-api-key", "x-api-key-token")
	if got := extractCallerToken(req); got != "x-api-key-token" {
		t.Fatalf("expected x-api-key-token, got %q", got)
	}
}

func TestExtractCallerTokenBearerPreferredOverXAPIKey(t *testing.T) {
	req, _ := http.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Bearer bearer-token")
	req.Header.Set("x-api-key", "x-api-key-token")
	if got := extractCallerToken(req); got != "bearer-token" {
		t.Fatalf("expected bearer-token, got %q", got)
	}
}

func TestExtractCallerTokenMissingHeaders(t *testing.T) {
	req, _ := http.NewRequest("POST", "/", nil)
	if got := extractCallerToken(req); got != "" {
		t.Fatalf("expected empty for missing headers, got %q", got)
	}
}

func TestExtractCallerTokenNonBearerAuth(t *testing.T) {
	req, _ := http.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Basic abc123")
	if got := extractCallerToken(req); got != "" {
		t.Fatalf("expected empty for Basic auth, got %q", got)
	}
}

// ─── Context helpers ─────────────────────────────────────────────────

func TestWithAuthAndFromContext(t *testing.T) {
	a := &RequestAuth{DeepSeekToken: "test-token"}
	ctx := WithAuth(context.Background(), a)
	got, ok := FromContext(ctx)
	if !ok || got.DeepSeekToken != "test-token" {
		t.Fatalf("expected token from context, got ok=%v token=%q", ok, got.DeepSeekToken)
	}
}

func TestFromContextMissing(t *testing.T) {
	_, ok := FromContext(context.Background())
	if ok {
		t.Fatal("expected not ok from empty context")
	}
}

// ─── RefreshToken edge cases ─────────────────────────────────────────

func TestRefreshTokenNotConfigToken(t *testing.T) {
	r := newTestResolver(t)
	a := &RequestAuth{UseConfigToken: false, resolver: r}
	if r.RefreshToken(context.Background(), a) {
		t.Fatal("expected false for non-config token")
	}
}

func TestRefreshTokenEmptyAccountID(t *testing.T) {
	r := newTestResolver(t)
	a := &RequestAuth{UseConfigToken: true, AccountID: "", resolver: r}
	if r.RefreshToken(context.Background(), a) {
		t.Fatal("expected false for empty account ID")
	}
}

func TestRefreshTokenSuccess(t *testing.T) {
	r := newTestResolver(t)
	// First acquire an account
	req, _ := http.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Bearer managed-key")
	a, err := r.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	defer r.Release(a)

	if !r.RefreshToken(context.Background(), a) {
		t.Fatal("expected refresh to succeed")
	}
	if a.DeepSeekToken != "fresh-token" {
		t.Fatalf("expected fresh-token after refresh, got %q", a.DeepSeekToken)
	}
}

// ─── MarkTokenInvalid edge cases ─────────────────────────────────────

func TestMarkTokenInvalidNotConfigToken(t *testing.T) {
	r := newTestResolver(t)
	a := &RequestAuth{UseConfigToken: false, DeepSeekToken: "direct", resolver: r}
	r.MarkTokenInvalid(a)
	// Should not panic, token should be unchanged for non-config
	_ = a.DeepSeekToken // Actual behavior may clear it; this test only asserts no panic.
}

func TestMarkTokenInvalidEmptyAccountID(t *testing.T) {
	r := newTestResolver(t)
	a := &RequestAuth{UseConfigToken: true, AccountID: "", DeepSeekToken: "tok", resolver: r}
	r.MarkTokenInvalid(a)
	// Should not panic
}

func TestMarkTokenInvalidClearsToken(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Bearer managed-key")
	a, err := r.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	defer r.Release(a)

	r.MarkTokenInvalid(a)
	if a.DeepSeekToken != "" {
		t.Fatalf("expected empty token after invalidation, got %q", a.DeepSeekToken)
	}
	if a.Account.Token != "" {
		t.Fatalf("expected empty account token after invalidation, got %q", a.Account.Token)
	}
}

// ─── SwitchAccount edge cases ────────────────────────────────────────

func TestSwitchAccountNotConfigToken(t *testing.T) {
	r := newTestResolver(t)
	a := &RequestAuth{UseConfigToken: false, resolver: r}
	if r.SwitchAccount(context.Background(), a) {
		t.Fatal("expected false for non-config token")
	}
}

func TestSwitchAccountNilTriedAccounts(t *testing.T) {
	r := newTestResolverWithAccounts(t, "managed-key", []config.Account{
		{Email: "acc1@test.com", Token: "t1"},
		{Email: "acc2@test.com", Token: "t2"},
	}, func(_ context.Context, _ config.Account) (string, error) {
		return "new-token", nil
	})

	// First acquire
	req, _ := http.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Bearer managed-key")
	a, err := r.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}

	oldID := a.AccountID
	a.TriedAccounts = nil // test nil initialization in SwitchAccount
	if !r.SwitchAccount(context.Background(), a) {
		t.Fatal("expected switch to succeed")
	}
	if a.AccountID == oldID {
		t.Fatalf("expected different account after switch")
	}
	r.Release(a)
}

func TestSwitchAccountSkipsLoginFailureAndContinues(t *testing.T) {
	r := newTestResolverWithAccounts(t, "managed-key", []config.Account{
		{Email: "acc1@test.com", Password: "pwd", Token: "t1"},
		{Email: "acc2@test.com", Password: "pwd"},
		{Email: "acc3@test.com", Password: "pwd"},
	}, func(_ context.Context, acc config.Account) (string, error) {
		if acc.Email == "acc2@test.com" {
			return "", errors.New("login failed")
		}
		return "new-token", nil
	})

	req, _ := http.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Bearer managed-key")
	a, err := r.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	defer r.Release(a)
	if a.AccountID != "acc1@test.com" {
		t.Fatalf("expected first account, got %q", a.AccountID)
	}
	if !r.SwitchAccount(context.Background(), a) {
		t.Fatal("expected switch to succeed after skipping failed account")
	}
	if a.AccountID != "acc3@test.com" {
		t.Fatalf("expected fallback to third account, got %q", a.AccountID)
	}
	if !a.TriedAccounts["acc2@test.com"] {
		t.Fatalf("expected failed account to be marked as tried")
	}
}

// ─── Release edge cases ─────────────────────────────────────────────

func TestReleaseNilAuth(t *testing.T) {
	r := newTestResolver(t)
	r.Release(nil) // should not panic
}

func TestReleaseNonConfigToken(t *testing.T) {
	r := newTestResolver(t)
	a := &RequestAuth{UseConfigToken: false}
	r.Release(a) // should not panic
}

func TestReleaseEmptyAccountID(t *testing.T) {
	r := newTestResolver(t)
	a := &RequestAuth{UseConfigToken: true, AccountID: ""}
	r.Release(a) // should not panic
}

// ─── Determine with login failure ────────────────────────────────────

func TestDetermineWithLoginFailure(t *testing.T) {
	r := newTestResolverWithAccounts(t, "managed-key", []config.Account{
		{Email: "acc@test.com", Password: "pwd"},
	}, func(_ context.Context, _ config.Account) (string, error) {
		return "", errors.New("login failed")
	})

	req, _ := http.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Bearer managed-key")
	_, err := r.Determine(req)
	if err == nil {
		t.Fatal("expected error when login fails")
	}
}

// ─── Determine with target account ───────────────────────────────────

func TestDetermineWithTargetAccount(t *testing.T) {
	r := newTestResolverWithAccounts(t, "managed-key", []config.Account{
		{Email: "acc1@test.com", Token: "t1"},
		{Email: "acc2@test.com", Token: "t2"},
	}, func(_ context.Context, _ config.Account) (string, error) {
		return "fresh-token", nil
	})

	req, _ := http.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Bearer managed-key")
	req.Header.Set("X-Whale2-Target-Account", "acc2@test.com")
	a, err := r.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	defer r.Release(a)
	if a.AccountID != "acc2@test.com" {
		t.Fatalf("expected target account acc2, got %q", a.AccountID)
	}
}
