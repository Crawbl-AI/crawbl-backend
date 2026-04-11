// Package e2e — agent token usage counter step definitions.
// These steps assert that agent turns are reflected in per-agent lifetime
// usage counters stored in Postgres. Steps degrade gracefully when no
// --database-dsn is provided so CI runs without a DB port-forward stay green.
package e2e

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"github.com/gocraft/dbr/v2"
	"github.com/tidwall/gjson"
)

// registerUsageCountersSteps binds all Gherkin phrases for usage counter assertions.
func registerUsageCountersSteps(sc *godog.ScenarioContext, tc *testContext) {
	sc.Step(`^the assistant's recorded usage should be zero for agent "([^"]*)"$`, tc.usageAssertZeroForAgent)
	sc.Step(`^the assistant's usage counter for agent "([^"]*)" should be captured as the baseline$`, tc.usageCaptureBaselineForAgent)
	sc.Step(`^the assistant should have consumed at least 1 token more than before within (\d+) seconds?$`, tc.usageAssertIncreasedFromBaseline)
	sc.Step(`^the workspace usage summary should show recent activity$`, tc.usageWorkspaceSummaryShowsActivity)
	sc.Step(`^user "([^"]*)" opens their monthly usage summary$`, tc.usageOpenUserSummary)
}

// usageOpenUserSummary hits GET /v1/users/usage/summary for the given
// alias. This endpoint aggregates per-user monthly token consumption
// and is what the mobile app reads to render the usage dial.
func (tc *testContext) usageOpenUserSummary(alias string) error {
	_, err := tc.doRequest("GET", "/v1/users/usage/summary", alias, nil)
	return err
}

// usageBaselineKey returns the map key used to store a baseline for a given
// user alias and agent slug. The colon separator prevents collisions between
// aliases whose names are prefixes of one another.
func usageBaselineKey(alias, slug string) string {
	return fmt.Sprintf("%s:%s", alias, normalizeKey(slug))
}

// usageTokensForAgent queries agent_usage_counters and returns the current
// tokens_used for the agent identified by slug under alias's workspace.
// Returns 0 if no row exists yet (first-turn case).
func (tc *testContext) usageTokensForAgent(alias, slug string) (int64, error) {
	s := tc.sess()
	if s == nil {
		return 0, nil
	}

	r, err := tc.resolveUser(alias)
	if err != nil {
		return 0, fmt.Errorf("usageTokensForAgent: %w", err)
	}

	var tokensUsed int64
	row := s.QueryRowContext(context.Background(), `
		SELECT COALESCE(auc.tokens_used, 0)
		FROM agent_usage_counters auc
		JOIN agents a ON a.id = auc.agent_id
		JOIN workspaces w ON w.id = a.workspace_id
		WHERE w.user_id = $1::uuid
		  AND a.slug = $2
		LIMIT 1`, r.UserID, normalizeKey(slug))

	if err := row.Scan(&tokensUsed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("querying agent_usage_counters for %s/%s: %w", alias, slug, err)
	}
	return tokensUsed, nil
}

// usageAssertZeroForAgent checks that the given agent has no recorded token
// usage for a freshly signed-up user. Passes silently when no DB is available.
func (tc *testContext) usageAssertZeroForAgent(slug string) error {
	return tc.withDB(func(_ *dbr.Session) error {
		got, err := tc.usageTokensForAgent("primary", slug)
		if err != nil {
			return fmt.Errorf("reading usage for agent %q: %w", slug, err)
		}
		if got != 0 {
			return fmt.Errorf("expected zero recorded tokens for agent %q on a fresh user, got %d", slug, got)
		}
		return nil
	})
}

// usageCaptureBaselineForAgent reads the current tokens_used for the agent
// and stores it so usageAssertIncreasedFromBaseline can compare later.
func (tc *testContext) usageCaptureBaselineForAgent(slug string) error {
	return tc.withDB(func(_ *dbr.Session) error {
		baseline, err := tc.usageTokensForAgent("primary", slug)
		if err != nil {
			return fmt.Errorf("capturing baseline for agent %q: %w", slug, err)
		}
		if tc.saved == nil {
			tc.saved = make(map[string]string)
		}
		tc.saved[usageBaselineKey("primary", slug)] = fmt.Sprintf("%d", baseline)
		return nil
	})
}

// usageAssertIncreasedFromBaseline polls until the tokens_used for the
// "manager" agent (the default swarm agent that handles top-level messages)
// exceeds the captured baseline, or the timeout expires.
//
// The agent slug "manager" is used because the swarm conversation routes to
// it for general messages. If a different agent replied the scenario will
// pass as long as any captured baseline was exceeded — the polling covers all
// agents whose baselines were captured in this scenario.
func (tc *testContext) usageAssertIncreasedFromBaseline(seconds int) error {
	return tc.withDB(func(_ *dbr.Session) error {
		// Collect all baselines captured in this scenario.
		type baselineEntry struct {
			alias    string
			slug     string
			baseline int64
		}
		var entries []baselineEntry

		// We only capture baselines for "primary" + "manager" in the scenario
		// but iterate over all saved keys to be forward-compatible.
		for key, val := range tc.saved {
			// Keys written by usageCaptureBaselineForAgent are "<alias>:<slug>".
			// Skip any tc.saved entries that are not in this format.
			alias, slug, ok := strings.Cut(key, ":")
			if !ok || alias == "" || slug == "" {
				continue
			}
			var baseline int64
			if _, err := fmt.Sscanf(val, "%d", &baseline); err != nil {
				continue
			}
			entries = append(entries, baselineEntry{alias: alias, slug: slug, baseline: baseline})
		}

		if len(entries) == 0 {
			// No baseline was captured — treat as a configuration error so the
			// scenario surfaces a meaningful failure rather than silently passing.
			return fmt.Errorf("no usage baseline captured; call 'the assistant's usage counter for agent X should be captured as the baseline' before this step")
		}

		return tc.pollFor(time.Duration(seconds)*time.Second, func() error {
			for _, e := range entries {
				current, err := tc.usageTokensForAgent(e.alias, e.slug)
				if err != nil {
					return fmt.Errorf("polling usage for agent %q: %w", e.slug, err)
				}
				if current > e.baseline {
					return nil
				}
			}
			// Build a summary for the timeout error message.
			var summary string
			for _, e := range entries {
				current, _ := tc.usageTokensForAgent(e.alias, e.slug)
				summary += fmt.Sprintf(" agent=%q baseline=%d current=%d;", e.slug, e.baseline, current)
			}
			return fmt.Errorf("token count has not increased from baseline:%s", summary)
		})
	})
}

// usageWorkspaceSummaryShowsActivity calls GET /v1/workspaces/{id}/usage and
// asserts that the response contains a positive tokens_used value, confirming
// that at least one turn was processed for the current workspace.
func (tc *testContext) usageWorkspaceSummaryShowsActivity() error {
	if err := tc.ensureDefaultWorkspace("primary"); err != nil {
		return err
	}
	state := tc.userState("primary")

	// Allow time for the async usage write to propagate before reading the summary.
	return tc.pollDefault(func() error {
		if _, err := tc.doRequest("GET", "/v1/workspaces/"+state.workspaceID+"/usage", "primary", nil); err != nil {
			return err
		}
		if err := tc.assertStatus(http.StatusOK); err != nil {
			return err
		}
		tokensUsed := gjson.GetBytes(tc.lastBody, "data.tokens_used").Int()
		if tokensUsed <= 0 {
			return fmt.Errorf("workspace usage summary shows tokens_used=%d, expected a positive value after a completed turn", tokensUsed)
		}
		return nil
	})
}
