// Package e2e — shared polling helper for async assertions.
//
// Agent-side tool effects (memory rows, session keys, Spaces objects)
// are not instantaneous — the runtime processes them asynchronously
// after a chat turn. pollUntil retries a check function with a bounded
// backoff so assertion steps can tolerate this latency without
// hard-coding arbitrary sleeps.
package e2e

import (
	"context"
	"time"
)

// pollUntil calls fn at interval until it returns nil or ctx expires.
// Returns the last error from fn if the context deadline is reached.
func pollUntil(ctx context.Context, interval time.Duration, fn func() error) error {
	// Run once immediately before starting the ticker.
	if err := fn(); err == nil {
		return nil
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastErr error
	for {
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return lastErr
			}
			return ctx.Err()
		case <-ticker.C:
			lastErr = fn()
			if lastErr == nil {
				return nil
			}
		}
	}
}
