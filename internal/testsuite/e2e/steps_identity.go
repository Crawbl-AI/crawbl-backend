// Package e2e — personal summary step definitions.
// These steps assert that the assistant's per-workspace personal summary
// is correctly stored and retrieved. The summary is set by a direct DB
// write (deterministic) and asserted via DB queries so CI remains fast
// and free from LLM non-determinism.
package e2e

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/cucumber/godog"
	"github.com/gocraft/dbr/v2"
)

// registerIdentitySteps binds all Gherkin phrases for the personal summary feature.
func registerIdentitySteps(sc *godog.ScenarioContext, tc *testContext) {
	sc.Step(`^the assistant should have no personal summary for "([^"]*)"$`, tc.identityAssertEmpty)
	sc.Step(`^the assistant learns that "([^"]*)" is "([^"]*)"$`, tc.identitySet)
	sc.Step(`^the assistant's personal summary for "([^"]*)" should mention "([^"]*)"$`, tc.identityAssertContains)
	sc.Step(`^the assistant's personal summary for "([^"]*)" should not mention "([^"]*)"$`, tc.identityAssertNotContains)

	sc.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		tc.identityTeardown()
		return ctx, nil
	})
}

// identityAssertEmpty asserts that no personal summary exists yet for the user.
func (tc *testContext) identityAssertEmpty(alias string) error {
	return tc.withDB(func(_ *dbr.Session) error {
		r, err := tc.resolveUser(alias)
		if err != nil {
			return err
		}
		if r.WorkspaceID == "" {
			return nil
		}
		count, err := tc.queryCount(
			`SELECT COUNT(*) FROM memory_identities WHERE workspace_id = $1`, r.WorkspaceID)
		if err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("expected no personal summary for %q, but one exists", alias)
		}
		return nil
	})
}

// identitySet upserts the personal summary for the user via a direct DB write.
func (tc *testContext) identitySet(alias, content string) error {
	return tc.withDB(func(s *dbr.Session) error {
		r, err := tc.resolveUser(alias)
		if err != nil {
			return err
		}
		if r.WorkspaceID == "" {
			return nil
		}
		// dbr's InsertBySql counts "?" placeholder occurrences, not
		// Postgres-style "$N" — the earlier version of this query used
		// $1/$2 and got "wrong placeholder count" every run. Using "?"
		// placeholders lets dbr drive the substitution normally.
		_, execErr := s.InsertBySql(
			`INSERT INTO memory_identities (workspace_id, content, updated_at)
			 VALUES (?, ?, NOW())
			 ON CONFLICT (workspace_id) DO UPDATE
			   SET content = EXCLUDED.content, updated_at = NOW()`,
			r.WorkspaceID, content,
		).ExecContext(context.Background())
		if execErr != nil {
			return fmt.Errorf("setting personal summary for %q: %w", alias, execErr)
		}
		return nil
	})
}

// identityAssertContains asserts that the stored summary contains the expected phrase.
func (tc *testContext) identityAssertContains(alias, phrase string) error {
	return tc.withDB(func(s *dbr.Session) error {
		r, err := tc.resolveUser(alias)
		if err != nil {
			return err
		}
		if r.WorkspaceID == "" {
			return nil
		}
		var content sql.NullString
		if err := s.QueryRowContext(context.Background(),
			`SELECT content FROM memory_identities WHERE workspace_id = $1`, r.WorkspaceID).Scan(&content); err != nil {
			return fmt.Errorf("reading personal summary for %q: %w", alias, err)
		}
		if !content.Valid || !strings.Contains(content.String, phrase) {
			return fmt.Errorf("expected personal summary for %q to mention %q, got: %q", alias, phrase, content.String)
		}
		return nil
	})
}

// identityAssertNotContains asserts that the stored summary does not contain the phrase.
func (tc *testContext) identityAssertNotContains(alias, phrase string) error {
	return tc.withDB(func(s *dbr.Session) error {
		r, err := tc.resolveUser(alias)
		if err != nil {
			return err
		}
		if r.WorkspaceID == "" {
			return nil
		}
		var content sql.NullString
		if err := s.QueryRowContext(context.Background(),
			`SELECT content FROM memory_identities WHERE workspace_id = $1`, r.WorkspaceID).Scan(&content); err != nil {
			// Row doesn't exist — phrase cannot be present.
			return nil
		}
		if content.Valid && strings.Contains(content.String, phrase) {
			return fmt.Errorf("expected personal summary for %q not to mention %q, but it does: %q", alias, phrase, content.String)
		}
		return nil
	})
}

// identityTeardown removes any personal summary rows written during the scenario
// so subsequent scenarios start from a clean state.
func (tc *testContext) identityTeardown() {
	if tc.dbConn == nil {
		return
	}
	for alias := range tc.users {
		r, err := tc.resolveUser(alias)
		if err != nil || r.WorkspaceID == "" {
			continue
		}
		s := tc.sess()
		if s == nil {
			continue
		}
		_, _ = s.DeleteFrom("memory_identities").
			Where("workspace_id = ?", r.WorkspaceID).
			ExecContext(context.Background())
	}
}
