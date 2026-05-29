package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"whale2api/internal/account"
	"whale2api/internal/config"
	"whale2api/internal/pooldb"
)

type ctxKey string

const authCtxKey ctxKey = "auth_context"

var (
	ErrUnauthorized  = errors.New("unauthorized: missing auth token")
	ErrInvalidAPIKey = errors.New("invalid API key: not registered in gateway pool")
	ErrNoAccount     = errors.New("no accounts configured or all accounts are busy")
	ErrPoolDBMissing = errors.New("gateway pool database is not configured")
)

type RequestAuth struct {
	UseConfigToken bool
	DeepSeekToken  string
	CallerID       string
	GatewayAPIKey  string
	AccountID      string
	Account        config.Account
	TriedAccounts  map[string]bool
	resolver       *Resolver
	activePool     *account.Pool
	PoolManaged    bool
}

type LoginFunc func(ctx context.Context, acc config.Account) (string, error)

type Resolver struct {
	Store  *config.Store
	PoolDB GatewayPool
	Login  LoginFunc

	mu               sync.Mutex
	tokenRefreshedAt map[string]time.Time
}

func NewResolver(store *config.Store, login LoginFunc) *Resolver {
	return &Resolver{
		Store:            store,
		Login:            login,
		tokenRefreshedAt: map[string]time.Time{},
	}
}

func (r *Resolver) poolFor(a *RequestAuth) *account.Pool {
	if r == nil || a == nil {
		return nil
	}
	return a.activePool
}

func (r *Resolver) Determine(req *http.Request) (*RequestAuth, error) {
	if r == nil || r.PoolDB == nil {
		return nil, ErrPoolDBMissing
	}
	callerKey := extractCallerToken(req)
	if callerKey == "" {
		return nil, ErrUnauthorized
	}
	ctx := req.Context()
	callerID := callerTokenID(callerKey)
	target := strings.TrimSpace(req.Header.Get("X-Whale2-Target-Account"))

	a, err := r.authFromPoolDB(ctx, callerKey, callerID, target)
	if err != nil {
		return nil, err
	}
	a.GatewayAPIKey = callerKey
	return a, nil
}

func (r *Resolver) authFromPoolDB(ctx context.Context, callerKey, callerID, target string) (*RequestAuth, error) {
	accounts, err := r.PoolDB.LoadAccountsForAPIKey(ctx, callerKey)
	if errors.Is(err, pooldb.ErrInvalidAPIKey) {
		return nil, ErrInvalidAPIKey
	}
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, ErrNoAccount
	}
	mem := account.NewMemoryLookup(accounts)
	subPool := account.NewPoolWithRuntime(mem, r.Store)
	return r.acquireManagedRequestAuth(ctx, callerID, target, subPool, true)
}

func (r *Resolver) acquireManagedRequestAuth(ctx context.Context, callerID, target string, pool *account.Pool, poolManaged bool) (*RequestAuth, error) {
	if pool == nil {
		return nil, ErrNoAccount
	}
	tried := map[string]bool{}
	var lastEnsureErr error
	for {
		if target == "" && len(tried) >= pool.AccountCount() {
			if lastEnsureErr != nil {
				return nil, lastEnsureErr
			}
			return nil, ErrNoAccount
		}
		acc, ok := pool.AcquireWait(ctx, target, tried)
		if !ok {
			if lastEnsureErr != nil {
				return nil, lastEnsureErr
			}
			return nil, ErrNoAccount
		}

		a := &RequestAuth{
			UseConfigToken: true,
			CallerID:       callerID,
			GatewayAPIKey:  "",
			AccountID:      acc.Identifier(),
			Account:        acc,
			TriedAccounts:  tried,
			resolver:       r,
			activePool:     pool,
			PoolManaged:    poolManaged,
		}

		if err := r.ensureManagedToken(ctx, a); err != nil {
			lastEnsureErr = err
			tried[a.AccountID] = true
			pool.Release(a.AccountID)
			if target != "" {
				return nil, err
			}
			continue
		}
		return a, nil
	}
}

// DetermineCaller resolves caller identity without acquiring any pooled account.
func (r *Resolver) DetermineCaller(req *http.Request) (*RequestAuth, error) {
	if r == nil || r.PoolDB == nil {
		return nil, ErrPoolDBMissing
	}
	callerKey := extractCallerToken(req)
	if callerKey == "" {
		return nil, ErrUnauthorized
	}
	callerID := callerTokenID(callerKey)
	ok, err := r.PoolDB.GatewayKeyExists(req.Context(), callerKey)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrInvalidAPIKey
	}
	return &RequestAuth{
		UseConfigToken: false,
		CallerID:       callerID,
		resolver:       r,
		TriedAccounts:  map[string]bool{},
		PoolManaged:    true,
	}, nil
}

func WithAuth(ctx context.Context, a *RequestAuth) context.Context {
	return context.WithValue(ctx, authCtxKey, a)
}

func FromContext(ctx context.Context) (*RequestAuth, bool) {
	v := ctx.Value(authCtxKey)
	a, ok := v.(*RequestAuth)
	return a, ok
}

func (r *Resolver) loginAndPersist(ctx context.Context, a *RequestAuth) error {
	token, err := r.Login(ctx, a.Account)
	if err != nil {
		r.TryAutoDiscardFromError(ctx, a, err)
		return err
	}
	a.Account.Token = token
	a.DeepSeekToken = token
	r.markTokenRefreshedNow(a.AccountID)
	if r.PoolDB == nil {
		return ErrPoolDBMissing
	}
	return r.PoolDB.UpdateAccountToken(ctx, a.AccountID, token)
}

func (r *Resolver) RefreshToken(ctx context.Context, a *RequestAuth) bool {
	if !a.UseConfigToken || a.AccountID == "" {
		return false
	}
	if r.PoolDB != nil {
		_ = r.PoolDB.ClearAccountToken(ctx, a.AccountID)
	}
	a.Account.Token = ""
	if err := r.loginAndPersist(ctx, a); err != nil {
		config.Logger.Error("[refresh_token] failed", "account", a.AccountID, "error", err)
		return false
	}
	return true
}

func (r *Resolver) MarkTokenInvalid(a *RequestAuth) {
	if !a.UseConfigToken || a.AccountID == "" {
		return
	}
	a.Account.Token = ""
	a.DeepSeekToken = ""
	r.clearTokenRefreshMark(a.AccountID)
	if r.PoolDB != nil {
		_ = r.PoolDB.ClearAccountToken(context.Background(), a.AccountID)
	}
}

func (r *Resolver) SwitchAccount(ctx context.Context, a *RequestAuth) bool {
	if !a.UseConfigToken {
		return false
	}
	pool := r.poolFor(a)
	if pool == nil {
		return false
	}
	if a.TriedAccounts == nil {
		a.TriedAccounts = map[string]bool{}
	}
	if a.AccountID != "" {
		a.TriedAccounts[a.AccountID] = true
		pool.Release(a.AccountID)
	}
	for {
		acc, ok := pool.Acquire("", a.TriedAccounts)
		if !ok {
			return false
		}
		a.Account = acc
		a.AccountID = acc.Identifier()
		if err := r.ensureManagedToken(ctx, a); err != nil {
			a.TriedAccounts[a.AccountID] = true
			pool.Release(a.AccountID)
			continue
		}
		return true
	}
}

// TryAutoDiscardHTTPBody classifies a completion HTTP body and discards muted/banned accounts.
func (a *RequestAuth) TryAutoDiscardHTTPBody(ctx context.Context, raw []byte) {
	if a == nil || a.resolver == nil {
		return
	}
	a.resolver.TryAutoDiscardFromHTTPBody(ctx, a, raw)
}

func (r *Resolver) Release(a *RequestAuth) {
	if a == nil || !a.UseConfigToken || a.AccountID == "" {
		return
	}
	if p := r.poolFor(a); p != nil {
		p.Release(a.AccountID)
	}
}

func (r *Resolver) ensureManagedToken(ctx context.Context, a *RequestAuth) error {
	if strings.TrimSpace(a.Account.Token) == "" {
		return r.loginAndPersist(ctx, a)
	}
	if r.shouldForceRefresh(a.AccountID) {
		return r.loginAndPersist(ctx, a)
	}
	a.DeepSeekToken = a.Account.Token
	return nil
}

func (r *Resolver) shouldForceRefresh(accountID string) bool {
	if r == nil || r.Store == nil {
		return false
	}
	if strings.TrimSpace(accountID) == "" {
		return false
	}
	intervalHours := r.Store.RuntimeTokenRefreshIntervalHours()
	if intervalHours <= 0 {
		return false
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	last, ok := r.tokenRefreshedAt[accountID]
	if !ok || last.IsZero() {
		r.tokenRefreshedAt[accountID] = now
		return false
	}
	return now.Sub(last) >= time.Duration(intervalHours)*time.Hour
}

func (r *Resolver) markTokenRefreshedNow(accountID string) {
	if r == nil || accountID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tokenRefreshedAt[accountID] = time.Now()
}

func (r *Resolver) clearTokenRefreshMark(accountID string) {
	if r == nil || accountID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tokenRefreshedAt, accountID)
}

func extractCallerToken(req *http.Request) string {
	if req == nil {
		return ""
	}
	authHeader := strings.TrimSpace(req.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		if token := strings.TrimSpace(authHeader[7:]); token != "" {
			return token
		}
	}
	if key := strings.TrimSpace(req.Header.Get("x-api-key")); key != "" {
		return key
	}
	if key := strings.TrimSpace(req.Header.Get("x-goog-api-key")); key != "" {
		return key
	}
	if key := strings.TrimSpace(req.URL.Query().Get("key")); key != "" {
		return key
	}
	return strings.TrimSpace(req.URL.Query().Get("api_key"))
}

func callerTokenID(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return "caller:" + hex.EncodeToString(sum[:8])
}
