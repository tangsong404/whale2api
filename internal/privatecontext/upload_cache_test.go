package privatecontext

import (
	"context"
	"sync"
	"testing"
	"time"

	"whale2api/internal/auth"
	dsclient "whale2api/internal/deepseek/client"
)

type cacheTestUploader struct {
	mu    sync.Mutex
	calls int
	block chan struct{}
}

func (u *cacheTestUploader) UploadFile(_ context.Context, _ *auth.RequestAuth, req dsclient.UploadFileRequest, _ int) (*dsclient.UploadFileResult, error) {
	if u.block != nil {
		<-u.block
	}
	u.mu.Lock()
	u.calls++
	n := u.calls
	u.mu.Unlock()
	return &dsclient.UploadFileResult{ID: "file-cache-" + string(rune('0'+n)), Filename: req.Filename}, nil
}

func resetUploadCacheForTest() {
	uploadCache.mu.Lock()
	defer uploadCache.mu.Unlock()
	uploadCache.entries = map[string]cacheEntry{}
	uploadCache.inflight = map[string]*uploadCall{}
	uploadCache.now = time.Now
}

func TestUploadCachesSuccessfulPrivateContextUpload(t *testing.T) {
	resetUploadCacheForTest()
	uploader := &cacheTestUploader{}
	a := &auth.RequestAuth{AccountID: "acct-a", DeepSeekToken: "token"}
	req := dsclient.UploadFileRequest{Filename: "opaque.txt", Data: []byte("context")}
	key := Key(a, "default", "context")

	first, err := Upload(context.Background(), uploader, a, req, 3, key)
	if err != nil {
		t.Fatalf("first upload failed: %v", err)
	}
	second, err := Upload(context.Background(), uploader, a, req, 3, key)
	if err != nil {
		t.Fatalf("second upload failed: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected cached file id, got %q and %q", first.ID, second.ID)
	}
	if uploader.calls != 1 {
		t.Fatalf("expected one underlying upload, got %d", uploader.calls)
	}
}

func TestKeyRequiresAccountID(t *testing.T) {
	if key := Key(&auth.RequestAuth{DeepSeekToken: "direct-token"}, "default", "context"); key != "" {
		t.Fatalf("expected no cache key without account id, got %q", key)
	}
}

func TestUploadCoalescesConcurrentPrivateContextUpload(t *testing.T) {
	resetUploadCacheForTest()
	uploader := &cacheTestUploader{block: make(chan struct{})}
	a := &auth.RequestAuth{AccountID: "acct-a", DeepSeekToken: "token"}
	req := dsclient.UploadFileRequest{Filename: "opaque.txt", Data: []byte("context")}
	key := Key(a, "default", "context")

	const callers = 3
	results := make(chan string, callers)
	for i := 0; i < callers; i++ {
		go func() {
			result, err := Upload(context.Background(), uploader, a, req, 3, key)
			if err != nil {
				results <- "err:" + err.Error()
				return
			}
			results <- result.ID
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(uploader.block)

	for i := 0; i < callers; i++ {
		got := <-results
		if got != "file-cache-1" {
			t.Fatalf("expected coalesced file id, got %q", got)
		}
	}
	if uploader.calls != 1 {
		t.Fatalf("expected one underlying upload, got %d", uploader.calls)
	}
}
