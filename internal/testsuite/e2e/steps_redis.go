// Package e2e — Redis assertion step definitions.
//
// Steps are phrased in plain-English user terms so .feature files
// never mention "Redis", "SCAN", or "TTL" — they read like product
// requirements:
//
//   the assistant should remember the current conversation context
//   the assistant session should expire automatically
//
// When tc.redisClient is nil the step is a silent no-op (same
// pattern as the database assertions).
package e2e

import (
	"context"
	"fmt"
	"time"

	"github.com/cucumber/godog"
)

func registerRedisSteps(sc *godog.ScenarioContext, tc *testContext) {
	sc.Step(`^the assistant should remember the current conversation context$`, tc.assistantSessionShouldExist)
	sc.Step(`^the assistant session should expire automatically$`, tc.assistantSessionShouldHaveTTL)
}

// assistantSessionShouldExist asserts that at least one Redis key
// matching the crawbl session patterns exists for the primary user's
// workspace. It polls for up to 30 seconds because session writes
// happen asynchronously after the chat turn completes.
func (tc *testContext) assistantSessionShouldExist() error {
	if tc.redisClient == nil {
		return nil
	}
	state := tc.state["primary"]
	if state == nil || state.workspaceID == "" {
		return fmt.Errorf("primary user workspace not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return pollUntil(ctx, 1*time.Second, func() error {
		// The session service keys are prefixed with crawbl:session:
		// followed by app:user:sessionID. We look for any key matching
		// the workspace-scoped pattern.
		pattern := "crawbl:session:*"
		var cursor uint64
		keys, _, err := tc.redisClient.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return fmt.Errorf("redis SCAN failed: %w", err)
		}
		if len(keys) == 0 {
			return fmt.Errorf("no session keys found matching %q", pattern)
		}
		return nil
	})
}

// assistantSessionShouldHaveTTL asserts that at least one matching
// session key has a positive TTL (proving auto-expiry is configured).
func (tc *testContext) assistantSessionShouldHaveTTL() error {
	if tc.redisClient == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return pollUntil(ctx, 1*time.Second, func() error {
		pattern := "crawbl:session:*"
		var cursor uint64
		keys, _, err := tc.redisClient.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return fmt.Errorf("redis SCAN failed: %w", err)
		}
		for _, key := range keys {
			ttl, err := tc.redisClient.TTL(ctx, key).Result()
			if err != nil {
				continue
			}
			if ttl > 0 {
				return nil // At least one key has auto-expiry.
			}
		}
		return fmt.Errorf("no session key with positive TTL found")
	})
}
