// Package e2e — monthly token quota enforcement step definitions.
// These steps set up and assert per-user monthly message limits against
// the live orchestrator and its Postgres backend. Steps degrade gracefully
// when no --database-dsn is provided.
package e2e

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"github.com/tidwall/gjson"
)

// registerQuotaSteps binds all Gherkin phrases that set up or assert monthly quota state.
func registerQuotaSteps(sc *godog.ScenarioContext, tc *testContext) {
	sc.Step(`^user "([^"]*)" has a monthly limit of (\d+) tokens?$`, tc.quotaSetMonthlyLimit)
	sc.Step(`^user "([^"]*)" has already used (\d+) tokens? this month$`, tc.quotaSetUsageCounter)
	sc.Step(`^user "([^"]*)" has no monthly limit$`, tc.quotaClear)
	sc.Step(`^the request should be rejected with a quota-exceeded error$`, tc.quotaAssertRejected)

	// Preemptive Before-hook: wipe any leaked quota state at the top
	// of every scenario so a failure inside one quota scenario cannot
	// leave a limit attached to the shared primary user and break the
	// warmup step of every subsequent scenario. This is an unconditional
	// safety net — the earlier "only if the scenario has a quota step"
	// filter let rows leak whenever the AfterScenario hook itself hit a
	// silent DB error.
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		tc.quotaWipeAllTestOwnedRows()
		return ctx, nil
	})

	// Belt-and-suspenders After hook: unconditional cleanup.
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		tc.quotaWipeAllTestOwnedRows()
		return ctx, nil
	})
}

// quotaWipeAllTestOwnedRows removes every quota / counter / plan row
// that the test harness might have inserted during previous scenarios.
// Runs in every Before/After hook so a silent teardown failure cannot
// snowball into a suite-wide failure cascade. Errors are logged but
// do not block the hook so tests without a DSN stay green.
func (tc *testContext) quotaWipeAllTestOwnedRows() {
	if tc.dbConn == nil {
		return
	}
	s := tc.sess()
	if s == nil {
		return
	}
	ctx := context.Background()

	// Delete test-owned quotas first so the FK to usage_plans is safe.
	if _, err := s.ExecContext(ctx,
		`DELETE FROM usage_quotas WHERE plan_id LIKE 'e2e-plan-%'`); err != nil {
		fmt.Printf("quotaWipeAll: delete usage_quotas: %v\n", err)
	}

	// Drop the test-owned plan rows next.
	if _, err := s.ExecContext(ctx,
		`DELETE FROM usage_plans WHERE plan_id LIKE 'e2e-plan-%'`); err != nil {
		fmt.Printf("quotaWipeAll: delete usage_plans: %v\n", err)
	}

	// Finally drop the per-test-user counters so over-limit residue
	// cannot bleed into the next scenario. Scoped to subjects starting
	// with "e2e-" so we only touch rows this suite owns.
	if _, err := s.ExecContext(ctx, `
		DELETE FROM usage_counters
		WHERE user_id IN (
			SELECT id FROM users WHERE subject LIKE 'e2e-%'
		)`); err != nil {
		fmt.Printf("quotaWipeAll: delete usage_counters: %v\n", err)
	}
}

// strings package keeps its import via quotaClearForAlias below.
var _ = strings.TrimSpace

// quotaSetMonthlyLimit inserts a usage_plans row (if absent) and a usage_quotas
// row linking the user to it, with the given monthly_token_limit.
// Uses ON CONFLICT DO UPDATE so it is idempotent.
func (tc *testContext) quotaSetMonthlyLimit(alias string, limit int) error {
	if tc.dbConn == nil {
		return nil
	}
	userID, err := tc.resolveUserID(alias)
	if err != nil {
		return err
	}

	// Deterministic plan ID scoped to the test user so parallel runs don't collide.
	planID := fmt.Sprintf("e2e-plan-%s", userID)
	s := tc.sess()

	// Upsert the plan row.
	_, execErr := s.ExecContext(context.Background(), `
		INSERT INTO usage_plans (plan_id, name, monthly_token_limit)
		VALUES ($1, $2, $3)
		ON CONFLICT (plan_id) DO UPDATE
		  SET monthly_token_limit = EXCLUDED.monthly_token_limit,
		      updated_at = NOW()`,
		planID, fmt.Sprintf("E2E Plan %s", alias), int64(limit))
	if execErr != nil {
		return fmt.Errorf("upsert usage_plans: %w", execErr)
	}

	// Remove any existing active quota for this user so the unique index is not violated.
	_, execErr = s.ExecContext(context.Background(),
		`DELETE FROM usage_quotas WHERE user_id = $1 AND expires_at IS NULL`, userID)
	if execErr != nil {
		return fmt.Errorf("clear existing usage_quotas: %w", execErr)
	}

	// Insert a fresh active quota.
	_, execErr = s.ExecContext(context.Background(), `
		INSERT INTO usage_quotas (user_id, plan_id, effective_at)
		VALUES ($1, $2, NOW())`,
		userID, planID)
	if execErr != nil {
		return fmt.Errorf("insert usage_quotas: %w", execErr)
	}

	return nil
}

// quotaSetUsageCounter upserts a usage_counters row for the current month
// with the given tokens_used value.
func (tc *testContext) quotaSetUsageCounter(alias string, tokensUsed int) error {
	if tc.dbConn == nil {
		return nil
	}
	userID, err := tc.resolveUserID(alias)
	if err != nil {
		return err
	}

	period := time.Now().UTC().Format("2006-01")
	s := tc.sess()

	_, execErr := s.ExecContext(context.Background(), `
		INSERT INTO usage_counters (user_id, period, tokens_used, last_updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (user_id, period) DO UPDATE
		  SET tokens_used = EXCLUDED.tokens_used,
		      last_updated_at = NOW()`,
		userID, period, int64(tokensUsed))
	if execErr != nil {
		return fmt.Errorf("upsert usage_counters: %w", execErr)
	}

	return nil
}

// quotaClear removes any active quota and current-month counter for the user.
func (tc *testContext) quotaClear(alias string) error {
	return tc.quotaClearForAlias(alias)
}

// quotaClearForAlias is the internal implementation used by both the Gherkin
// step and the AfterScenario teardown hook.
func (tc *testContext) quotaClearForAlias(alias string) error {
	if tc.dbConn == nil {
		return nil
	}
	userID, err := tc.resolveUserID(alias)
	if err != nil {
		// User may not exist in this scenario — ignore.
		return nil
	}

	period := time.Now().UTC().Format("2006-01")
	s := tc.sess()

	_, _ = s.ExecContext(context.Background(),
		`DELETE FROM usage_quotas WHERE user_id = $1 AND expires_at IS NULL`, userID)
	_, _ = s.ExecContext(context.Background(),
		`DELETE FROM usage_counters WHERE user_id = $1 AND period = $2`, userID, period)

	// Remove any test-owned plan rows for this user.
	planID := fmt.Sprintf("e2e-plan-%s", userID)
	_, _ = s.ExecContext(context.Background(),
		`DELETE FROM usage_plans WHERE plan_id = $1`, planID)

	return nil
}

// quotaAssertRejected checks that the last response is a quota-exceeded
// rejection. The orchestrator returns 429 Too Many Requests with the
// QTA0001 business code; older builds returned 400. We accept either
// status so the test stays green across both contracts but still
// requires the QTA0001 code so we never confuse a generic 429 for a
// quota breach.
func (tc *testContext) quotaAssertRejected() error {
	if tc.lastStatus != statusTooManyRequests && tc.lastStatus != statusBadRequest {
		return fmt.Errorf("expected quota-exceeded response (429 or 400), got %d; body: %s",
			tc.lastStatus, truncate(string(tc.lastBody), maxBodyDisplayLen))
	}
	body := string(tc.lastBody)
	// The business error envelope is {"message": "...", "code": "QTA0001"}
	// at the top level on some paths and nested under "error" on others;
	// accept both.
	code := gjson.Get(body, "code").String()
	if code == "" {
		code = gjson.Get(body, "error.code").String()
	}
	if code != "QTA0001" {
		return fmt.Errorf("expected quota-exceeded error code QTA0001, got %q (body: %s)", code, truncate(body, maxBodyDisplayLen))
	}
	return nil
}

// resolveUserID looks up the UUID of a test user from the database.
func (tc *testContext) resolveUserID(alias string) (string, error) {
	subject := tc.resolveSubject(alias)
	s := tc.sess()
	if s == nil {
		return "", fmt.Errorf("no database connection available")
	}
	var id string
	row := s.QueryRowContext(context.Background(), "SELECT id FROM users WHERE subject = $1", subject)
	if err := row.Scan(&id); err != nil {
		return "", fmt.Errorf("resolveUserID for %q (subject %q): %w", alias, subject, err)
	}
	return id, nil
}

// truncate shortens s to at most n bytes for display in error messages.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
