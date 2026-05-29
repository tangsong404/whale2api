package client

import "context"

const defaultGlobalUploadConcurrency = 16

type uploadSemaphore chan struct{}

func newUploadSemaphore(limit int) uploadSemaphore {
	if limit <= 0 {
		limit = defaultGlobalUploadConcurrency
	}
	return make(uploadSemaphore, limit)
}

func (s uploadSemaphore) acquire(ctx context.Context) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case s <- struct{}{}:
		return func() { <-s }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

var globalUploadLimiter = newUploadSemaphore(defaultGlobalUploadConcurrency)
