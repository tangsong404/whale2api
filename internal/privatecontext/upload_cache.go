package privatecontext

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"whale2api/internal/auth"
	dsclient "whale2api/internal/deepseek/client"
)

const cacheTTL = 20 * time.Minute

type Uploader interface {
	UploadFile(ctx context.Context, a *auth.RequestAuth, req dsclient.UploadFileRequest, maxAttempts int) (*dsclient.UploadFileResult, error)
}

type cacheEntry struct {
	result    dsclient.UploadFileResult
	expiresAt time.Time
}

type uploadCall struct {
	done   chan struct{}
	result *dsclient.UploadFileResult
	err    error
}

var uploadCache = struct {
	mu       sync.Mutex
	entries  map[string]cacheEntry
	inflight map[string]*uploadCall
	now      func() time.Time
}{
	entries:  map[string]cacheEntry{},
	inflight: map[string]*uploadCall{},
	now:      time.Now,
}

func Key(a *auth.RequestAuth, modelType string, contextText string) string {
	accountKey := ""
	if a != nil {
		accountKey = strings.TrimSpace(a.AccountID)
	}
	if accountKey == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(contextText))
	return strings.TrimSpace(accountKey) + "|" + strings.ToLower(strings.TrimSpace(modelType)) + "|" + hex.EncodeToString(sum[:])
}

func Upload(ctx context.Context, uploader Uploader, a *auth.RequestAuth, req dsclient.UploadFileRequest, maxAttempts int, key string) (*dsclient.UploadFileResult, error) {
	key = strings.TrimSpace(key)
	if uploader == nil {
		return nil, nil
	}
	if key == "" {
		return uploader.UploadFile(ctx, a, req, maxAttempts)
	}

	now := uploadCache.now()
	uploadCache.mu.Lock()
	if entry, ok := uploadCache.entries[key]; ok {
		if now.Before(entry.expiresAt) && strings.TrimSpace(entry.result.ID) != "" {
			result := entry.result
			uploadCache.mu.Unlock()
			return &result, nil
		}
		delete(uploadCache.entries, key)
	}
	if call := uploadCache.inflight[key]; call != nil {
		uploadCache.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-call.done:
			if call.result == nil {
				return nil, call.err
			}
			result := *call.result
			return &result, call.err
		}
	}
	call := &uploadCall{done: make(chan struct{})}
	uploadCache.inflight[key] = call
	uploadCache.mu.Unlock()

	result, err := uploader.UploadFile(ctx, a, req, maxAttempts)

	uploadCache.mu.Lock()
	if result != nil && err == nil && strings.TrimSpace(result.ID) != "" {
		uploadCache.entries[key] = cacheEntry{result: *result, expiresAt: uploadCache.now().Add(cacheTTL)}
	}
	call.result = result
	call.err = err
	delete(uploadCache.inflight, key)
	close(call.done)
	uploadCache.mu.Unlock()

	if result == nil {
		return nil, err
	}
	copyResult := *result
	return &copyResult, err
}
