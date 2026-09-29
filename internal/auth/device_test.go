package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"whale2api/internal/config"
	"whale2api/internal/deviceharvest"
	"whale2api/internal/poolaccounthealth"
	"whale2api/internal/pooldb"
)

type fakeHarvester struct {
	enabled bool
	tokens  []string
	err     error
	started chan struct{}
	release chan struct{}

	mu    sync.Mutex
	calls int
	ids   []string
}

func (f *fakeHarvester) Enabled() bool { return f.enabled }

func (f *fakeHarvester) Harvest(_ context.Context, accountID string) (string, error) {
	f.mu.Lock()
	f.calls++
	idx := f.calls - 1
	f.ids = append(f.ids, accountID)
	f.mu.Unlock()
	if f.started != nil {
		select {
		case f.started <- struct{}{}:
		default:
		}
	}
	if f.release != nil {
		<-f.release
	}
	if f.err != nil {
		return "", f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if idx < len(f.tokens) {
		return f.tokens[idx], nil
	}
	return "B-harvested-default", nil
}

func (f *fakeHarvester) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeHarvester) harvestedAccounts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.ids...)
}

type fakeDeviceRejection struct{ msg string }

func (e *fakeDeviceRejection) Error() string        { return e.msg }
func (e *fakeDeviceRejection) DeviceRejected() bool { return true }

func TestLoginWithDeviceHarvestsAndPersists(t *testing.T) {
	ctx := context.Background()
	harvester := &fakeHarvester{enabled: true, tokens: []string{"B-harvested-1"}}
	var seen []string
	var mu sync.Mutex
	r := newTestResolverWithAccounts(t, "managed-key", []config.Account{{Email: "acc@example.com", Password: "pwd"}},
		func(_ context.Context, acc config.Account) (string, error) {
			mu.Lock()
			seen = append(seen, acc.DeviceID)
			mu.Unlock()
			return "upstream-token", nil
		})
	r.DeviceHarvest = harvester

	token, err := r.LoginWithDevice(ctx, config.Account{Email: "acc@example.com", Password: "pwd"})
	if err != nil {
		t.Fatal(err)
	}
	if token != "upstream-token" {
		t.Fatalf("token = %q", token)
	}
	if got := harvester.callCount(); got != 1 {
		t.Fatalf("expected 1 harvest, got %d", got)
	}
	mu.Lock()
	gotDevice := ""
	if len(seen) > 0 {
		gotDevice = seen[0]
	}
	mu.Unlock()
	if gotDevice != "B-harvested-1" {
		t.Fatalf("login saw device_id = %q", gotDevice)
	}
	accounts, err := r.PoolDB.LoadAccountsForAPIKey(ctx, "managed-key")
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || accounts[0].DeviceID != "B-harvested-1" {
		t.Fatalf("device id not persisted: %+v", accounts)
	}
}

func TestLoginWithDeviceSingleflightPerAccount(t *testing.T) {
	ctx := context.Background()
	harvester := &fakeHarvester{enabled: true, started: make(chan struct{}, 1), release: make(chan struct{})}
	r := newTestResolverWithAccounts(t, "managed-key", []config.Account{{Email: "acc@example.com", Password: "pwd"}},
		func(_ context.Context, acc config.Account) (string, error) {
			return "tok-" + acc.DeviceID, nil
		})
	r.DeviceHarvest = harvester

	acc := config.Account{Email: "acc@example.com", Password: "pwd"}
	var wg sync.WaitGroup
	tokens := make(chan string, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := r.LoginWithDevice(ctx, acc)
			tokens <- token
			errs <- err
		}()
	}
	<-harvester.started
	// Give the second caller time to join the in-flight harvest call.
	time.Sleep(100 * time.Millisecond)
	if got := harvester.callCount(); got != 1 {
		t.Fatalf("concurrent logins triggered %d harvests, want 1", got)
	}
	close(harvester.release)
	wg.Wait()
	close(tokens)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("login error: %v", err)
		}
	}
	for token := range tokens {
		if token != "tok-B-harvested-default" {
			t.Fatalf("token = %q", token)
		}
	}
	if got := harvester.callCount(); got != 1 {
		t.Fatalf("expected exactly 1 harvest after completion, got %d", got)
	}
}

func TestLoginWithDeviceRetriesOnceAfterDeviceRejection(t *testing.T) {
	ctx := context.Background()
	harvester := &fakeHarvester{enabled: true, tokens: []string{"B-first", "B-second"}}
	var logins []string
	var mu sync.Mutex
	r := newTestResolverWithAccounts(t, "managed-key", []config.Account{{Email: "acc@example.com", Password: "pwd"}},
		func(_ context.Context, acc config.Account) (string, error) {
			mu.Lock()
			logins = append(logins, acc.DeviceID)
			n := len(logins)
			mu.Unlock()
			if n == 1 {
				return "", &fakeDeviceRejection{msg: "login: RISK_DEVICE_DETECTED"}
			}
			return "upstream-token", nil
		})
	r.DeviceHarvest = harvester

	token, err := r.LoginWithDevice(ctx, config.Account{Email: "acc@example.com", Password: "pwd"})
	if err != nil {
		t.Fatal(err)
	}
	if token != "upstream-token" {
		t.Fatalf("token = %q", token)
	}
	if got := harvester.callCount(); got != 2 {
		t.Fatalf("expected re-harvest, got %d harvest calls", got)
	}
	mu.Lock()
	gotLogins := append([]string(nil), logins...)
	mu.Unlock()
	if len(gotLogins) != 2 || gotLogins[0] != "B-first" || gotLogins[1] != "B-second" {
		t.Fatalf("login sequence = %v, want [B-first B-second]", gotLogins)
	}
	accounts, err := r.PoolDB.LoadAccountsForAPIKey(ctx, "managed-key")
	if err != nil {
		t.Fatal(err)
	}
	if accounts[0].DeviceID != "B-second" {
		t.Fatalf("persisted device_id = %q, want B-second", accounts[0].DeviceID)
	}
}

func TestLoginWithDeviceStillRejectedReturnsNoUsableToken(t *testing.T) {
	ctx := context.Background()
	harvester := &fakeHarvester{enabled: true, tokens: []string{"B-first", "B-second"}}
	r := newTestResolverWithAccounts(t, "managed-key", []config.Account{{Email: "acc@example.com", Password: "pwd"}},
		func(_ context.Context, _ config.Account) (string, error) {
			return "", &fakeDeviceRejection{msg: "login: RISK_DEVICE_DETECTED"}
		})
	r.DeviceHarvest = harvester

	_, err := r.LoginWithDevice(ctx, config.Account{Email: "acc@example.com", Password: "pwd"})
	if !errors.Is(err, ErrNoUsableDeviceToken) {
		t.Fatalf("expected ErrNoUsableDeviceToken, got %v", err)
	}
	if errors.Is(err, ErrDeviceHarvestUnavailable) {
		t.Fatalf("device rejection misreported as harvest failure: %v", err)
	}
	if got := harvester.callCount(); got != 2 {
		t.Fatalf("expected exactly 2 harvest attempts, got %d", got)
	}
	if reason := poolaccounthealth.ClassifyLoginError(err.Error()); reason != "" {
		t.Fatalf("device rejection must not be classified as mute/ban, got %q", reason)
	}
}

func TestLoginWithDeviceHarvestUnavailableFailsFast(t *testing.T) {
	ctx := context.Background()
	harvester := &fakeHarvester{enabled: true, err: deviceharvest.ErrDeviceHarvestUnavailable}
	loginCalls := 0
	r := newTestResolverWithAccounts(t, "managed-key", []config.Account{{Email: "acc@example.com", Password: "pwd"}},
		func(_ context.Context, _ config.Account) (string, error) {
			loginCalls++
			return "upstream-token", nil
		})
	r.DeviceHarvest = harvester

	_, err := r.LoginWithDevice(ctx, config.Account{Email: "acc@example.com", Password: "pwd"})
	if !errors.Is(err, ErrDeviceHarvestUnavailable) {
		t.Fatalf("expected ErrDeviceHarvestUnavailable, got %v", err)
	}
	if loginCalls != 0 {
		t.Fatalf("login must not run when harvesting fails without a token, got %d calls", loginCalls)
	}
	if got := harvester.callCount(); got != 1 {
		t.Fatalf("harvest must not be retried in a loop, got %d calls", got)
	}
}

// TestDetermineStopsAccountLoopWhenHarvestUnavailable guards against walking
// the whole pool (one harvest timeout per account) when the harvest service is
// down; the failure is account-independent and must terminate the loop.
func TestDetermineStopsAccountLoopWhenHarvestUnavailable(t *testing.T) {
	harvester := &fakeHarvester{enabled: true, err: deviceharvest.ErrDeviceHarvestUnavailable}
	loginCalls := 0
	r := newTestResolverWithAccounts(t, "managed-key", []config.Account{
		{Email: "first@example.com", Password: "pwd"},
		{Email: "second@example.com", Password: "pwd"},
	}, func(_ context.Context, _ config.Account) (string, error) {
		loginCalls++
		return "upstream-token", nil
	})
	r.DeviceHarvest = harvester

	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("x-api-key", "managed-key")
	a, err := r.Determine(req)
	if a != nil {
		r.Release(a)
		t.Fatal("expected no pooled account, got one")
	}
	if !errors.Is(err, ErrDeviceHarvestUnavailable) {
		t.Fatalf("expected ErrDeviceHarvestUnavailable, got %v", err)
	}
	if loginCalls != 0 {
		t.Fatalf("login calls = %d, want 0", loginCalls)
	}
	if got := harvester.callCount(); got != 1 {
		t.Fatalf("harvest calls = %d, want exactly 1 (must not iterate the pool)", got)
	}
	if got := harvester.harvestedAccounts(); len(got) != 1 || got[0] != "first@example.com" {
		t.Fatalf("harvested accounts = %v, want only the first account", got)
	}
}

// TestSwitchAccountStopsWhenHarvestUnavailable covers the same terminal
// condition on the SwitchAccount path: it must stop instead of harvesting for
// every remaining account.
func TestSwitchAccountStopsWhenHarvestUnavailable(t *testing.T) {
	ctx := context.Background()
	harvester := &fakeHarvester{enabled: true, err: deviceharvest.ErrDeviceHarvestUnavailable}
	loginCalls := 0
	r := newTestResolverWithAccounts(t, "managed-key", []config.Account{
		{Email: "first@example.com", Password: "pwd", Token: "tok-first"},
		{Email: "second@example.com", Password: "pwd"},
	}, func(_ context.Context, _ config.Account) (string, error) {
		loginCalls++
		return "upstream-token", nil
	})
	r.DeviceHarvest = harvester

	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("x-api-key", "managed-key")
	a, err := r.Determine(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release(a)
	if a.AccountID != "first@example.com" {
		t.Fatalf("initial account = %q, want first@example.com", a.AccountID)
	}
	if switched := a.SwitchAccount(ctx); switched {
		t.Fatal("SwitchAccount must fail fast when the harvest service is down")
	}
	if loginCalls != 0 {
		t.Fatalf("login calls = %d, want 0", loginCalls)
	}
	if got := harvester.callCount(); got != 1 {
		t.Fatalf("harvest calls = %d, want exactly 1", got)
	}
	if got := harvester.harvestedAccounts(); len(got) != 1 || got[0] != "second@example.com" {
		t.Fatalf("harvested accounts = %v, want only the second account", got)
	}
}

func TestLoginWithoutHarvestServiceKeepsLegacyBehavior(t *testing.T) {
	ctx := context.Background()
	var seenDevice string
	r := newTestResolverWithAccounts(t, "managed-key", []config.Account{{Email: "acc@example.com", Password: "pwd"}},
		func(_ context.Context, acc config.Account) (string, error) {
			seenDevice = acc.DeviceID
			return "upstream-token", nil
		})
	// No DeviceHarvest configured: login proceeds without a token and no panic.
	token, err := r.LoginWithDevice(ctx, config.Account{Email: "acc@example.com", Password: "pwd"})
	if err != nil {
		t.Fatal(err)
	}
	if token != "upstream-token" || seenDevice != "" {
		t.Fatalf("token=%q device=%q", token, seenDevice)
	}
}

func TestDetermineHarvestsDeviceIDBeforeLogin(t *testing.T) {
	ctx := context.Background()
	harvester := &fakeHarvester{enabled: true, tokens: []string{"B-dynamic"}}
	var loginDevice string
	r := newTestResolverWithAccounts(t, "managed-key", []config.Account{{Email: "acc@example.com", Password: "pwd"}},
		func(_ context.Context, acc config.Account) (string, error) {
			loginDevice = acc.DeviceID
			return "upstream-token", nil
		})
	r.DeviceHarvest = harvester

	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("x-api-key", "managed-key")
	a, err := r.Determine(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release(a)
	if a.DeepSeekToken != "upstream-token" {
		t.Fatalf("token = %q", a.DeepSeekToken)
	}
	if loginDevice != "B-dynamic" || a.Account.DeviceID != "B-dynamic" {
		t.Fatalf("login device = %q, auth device = %q", loginDevice, a.Account.DeviceID)
	}
	accounts, err := r.PoolDB.LoadAccountsForAPIKey(ctx, "managed-key")
	if err != nil {
		t.Fatal(err)
	}
	if accounts[0].DeviceID != "B-dynamic" {
		t.Fatalf("device id not persisted: %+v", accounts)
	}
}

func TestEnsureAccountDeviceIDKeepsExistingToken(t *testing.T) {
	harvester := &fakeHarvester{enabled: true}
	r := newTestResolverWithAccounts(t, "managed-key", []config.Account{{Email: "acc@example.com", Password: "pwd"}},
		func(_ context.Context, _ config.Account) (string, error) { return "tok", nil })
	r.DeviceHarvest = harvester

	acc, err := r.EnsureAccountDeviceID(context.Background(), config.Account{Email: "acc@example.com", DeviceID: "B-existing"})
	if err != nil {
		t.Fatal(err)
	}
	if acc.DeviceID != "B-existing" {
		t.Fatalf("device_id = %q", acc.DeviceID)
	}
	if got := harvester.callCount(); got != 0 {
		t.Fatalf("existing token must not trigger a harvest, got %d calls", got)
	}
}

// TestDetermineHarvestsThroughHTTPServiceAndSQLite exercises the full on-demand
// chain with the real pooldb queries and a real HTTP harvest client.
func TestDetermineHarvestsThroughHTTPServiceAndSQLite(t *testing.T) {
	ctx := context.Background()
	db, err := pooldb.Connect(ctx, filepath.Join(t.TempDir(), "pool.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.CreateGatewayKey(ctx, "managed-key", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := db.AddAccountToPool(ctx, "managed-key", "acc@example.com", "pwd", ""); err != nil {
		t.Fatal(err)
	}

	var harvestCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&harvestCalls, 1)
		_, _ = w.Write([]byte(`{"device_id":"B-http-token"}`))
	}))
	defer srv.Close()

	var seenDevice string
	r := NewResolver(config.LoadStore(), func(_ context.Context, acc config.Account) (string, error) {
		seenDevice = acc.DeviceID
		return "upstream-token", nil
	})
	r.PoolDB = db
	r.DeviceHarvest = deviceharvest.New(srv.URL, "", time.Second)

	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("x-api-key", "managed-key")
	a, err := r.Determine(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release(a)
	if seenDevice != "B-http-token" || a.DeepSeekToken != "upstream-token" {
		t.Fatalf("login device = %q, token = %q", seenDevice, a.DeepSeekToken)
	}
	if harvestCalls != 1 {
		t.Fatalf("harvest calls = %d, want 1", harvestCalls)
	}
	cred, err := db.GetPoolAccountCredential(ctx, "managed-key", "acc@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if cred.DeviceID != "B-http-token" {
		t.Fatalf("persisted device_id = %q", cred.DeviceID)
	}
}

// TestEnsureAccountDeviceIDPersistsForMobileIdentifier is the regression for the
// identifier contract: AccountToConfig keeps a raw mobile for pool lookups while
// the login credential stays a mobile, so harvesting must still persist.
func TestEnsureAccountDeviceIDPersistsForMobileIdentifier(t *testing.T) {
	ctx := context.Background()
	const identifier = "13800138000"

	db, err := pooldb.Connect(ctx, filepath.Join(t.TempDir(), "mobile.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.CreateGatewayKey(ctx, "managed-key", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := db.AddAccountToPool(ctx, "managed-key", identifier, "pwd", ""); err != nil {
		t.Fatal(err)
	}

	harvester := &fakeHarvester{enabled: true, tokens: []string{"B-mobile-1", "B-mobile-2"}}
	r := NewResolver(config.LoadStore(), func(_ context.Context, _ config.Account) (string, error) {
		return "upstream-token", nil
	})
	r.PoolDB = db
	r.DeviceHarvest = harvester

	loadAcc := func() config.Account {
		t.Helper()
		cred, err := db.GetPoolAccountCredential(ctx, "managed-key", identifier)
		if err != nil {
			t.Fatal(err)
		}
		acc := pooldb.AccountToConfig(cred.Identifier, cred.Password, cred.DeviceID)
		if got := acc.Identifier(); got != identifier {
			t.Fatalf("Identifier() = %q, want raw mobile %q", got, identifier)
		}
		if acc.Mobile != identifier || acc.Email != "" {
			t.Fatalf("login credential changed: %+v", acc)
		}
		return acc
	}

	got, err := r.EnsureAccountDeviceID(ctx, loadAcc())
	if err != nil {
		t.Fatal(err)
	}
	if got.DeviceID != "B-mobile-1" {
		t.Fatalf("harvested device_id = %q", got.DeviceID)
	}
	accounts, err := db.LoadAccountsForAPIKey(ctx, "managed-key")
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || accounts[0].DeviceID != "B-mobile-1" {
		t.Fatalf("device_id not persisted for mobile account: %+v", accounts)
	}
	if accounts[0].Identifier() != identifier || accounts[0].PoolIdentifier != identifier {
		t.Fatalf("raw identifier lost when loading accounts: %+v", accounts[0])
	}
	rows, err := db.ListPoolAccountsAll(ctx, "managed-key", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !rows[0].HasDeviceID || rows[0].DeviceIDUpdatedAt.IsZero() {
		t.Fatalf("device status not exposed: %+v", rows)
	}

	// Clearing the token must lead to a fresh harvest and a persisted update.
	if err := db.ClearAccountDeviceID(ctx, identifier); err != nil {
		t.Fatal(err)
	}
	got, err = r.EnsureAccountDeviceID(ctx, loadAcc())
	if err != nil {
		t.Fatal(err)
	}
	if got.DeviceID != "B-mobile-2" {
		t.Fatalf("re-harvested device_id = %q", got.DeviceID)
	}
	cred, err := db.GetPoolAccountCredential(ctx, "managed-key", identifier)
	if err != nil {
		t.Fatal(err)
	}
	if cred.DeviceID != "B-mobile-2" {
		t.Fatalf("re-harvested device_id not persisted: %q", cred.DeviceID)
	}
	if got := harvester.callCount(); got != 2 {
		t.Fatalf("harvest calls = %d, want 2", got)
	}
}
