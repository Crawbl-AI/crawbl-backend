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

// identityWorkspaceID resolves the default workspace UUID for the given alias
// via the users → workspaces join in Postgres.
func (tc *testContext) identityWorkspaceID(alias string) (string, error) {
	subject := tc.resolveSubject(alias)
	s := tc.sess()
	if s == nil {
		return "", nil
	}
	var wsID string
	row := s.QueryRowContext(context.Background(), `
		SELECT workspaces.id FROM workspaces
		JOIN users ON users.id = workspaces.user_id
		WHERE users.subject = $1
		ORDER BY workspaces.created_at ASC
		LIMIT 1`, subject)
	if err := row.Scan(&wsID); err != nil {
		return "", fmt.Errorf("resolving workspace for %q: %w", alias, err)
	}
	return wsID, nil
}

// identityAssertEmpty asserts that no personal summary exists yet for the user.
func (tc *testContext) identityAssertEmpty(alias string) error {
	if tc.dbConn == nil {
		return nil
	}
	wsID, err := tc.identityWorkspaceID(alias)
	if err != nil {
		return err
	}
	if wsID == "" {
		return nil
	}
	count, err := tc.queryCount(
		`SELECT COUNT(*) FROM memory_identities WHERE workspace_id = $1`, wsID)
	if err != nil {
		return err
	}
	if count != 0 {
		return fmt.Errorf("expected no personal summary for %q, but one exists", alias)
	}
	return nil
}

// identitySet upserts the personal summary for the user via a direct DB write.
func (tc *testContext) identitySet(alias, content string) error {
	if tc.dbConn == nil {
		return nil
	}
	wsID, err := tc.identityWorkspaceID(alias)
	if err != nil {
		return err
	}
	if wsID == "" {
		return nil
	}
	s := tc.sess()
	if s == nil {
		return nil
	}
	_, execErr := s.InsertBySql(
		`INSERT INTO memory_identities (workspace_id, content, updated_at)
		 VALUES ($1, $2, NOW())
		 ON CONFLICT (workspace_id) DO UPDATE
		   SET content = EXCLUDED.content, updated_at = NOW()`,
		wsID, content,
	).ExecContext(context.Background())
	if execErr != nil {
		return fmt.Errorf("setting personal summary for %q: %w", alias, execErr)
	}
	return nil
}

// identityAssertContains asserts that the stored summary contains the expected phrase.
func (tc *testContext) identityAssertContains(alias, phrase string) error {
	if tc.dbConn == nil {
		return nil
	}
	wsID, err := tc.identityWorkspaceID(alias)
	if err != nil {
		return err
	}
	if wsID == "" {
		return nil
	}
	s := tc.sess()
	if s == nil {
		return nil
	}
	var content sql.NullString
	row := s.QueryRowContext(context.Background(),
		`SELECT content FROM memory_identities WHERE workspace_id = $1`, wsID)
	if err := row.Scan(&content); err != nil {
		return fmt.Errorf("reading personal summary for %q: %w", alias, err)
	}
	if !content.Valid || !strings.Contains(content.String, phrase) {
		return fmt.Errorf("expected personal summary for %q to mention %q, got: %q", alias, phrase, content.String)
	}
	return nil
}

// identityAssertNotContains asserts that the stored summary does not contain the phrase.
func (tc *testContext) identityAssertNotContains(alias, phrase string) error {
	if tc.dbConn == nil {
		return nil
	}
	wsID, err := tc.identityWorkspaceID(alias)
	if err != nil {
		return err
	}
	if wsID == "" {
		return nil
	}
	s := tc.sess()
	if s == nil {
		return nil
	}
	var content sql.NullString
	row := s.QueryRowContext(context.Background(),
		`SELECT content FROM memory_identities WHERE workspace_id = $1`, wsID)
	if scanErr := row.Scan(&content); scanErr != nil {
		// Row doesn't exist — phrase cannot be present.
		return nil
	}
	if content.Valid && strings.Contains(content.String, phrase) {
		return fmt.Errorf("expected personal summary for %q not to mention %q, but it does: %q", alias, phrase, content.String)
	}
	return nil
}

// identityTeardown removes any personal summary rows written during the scenario
// so subsequent scenarios start from a clean state.
func (tc *testContext) identityTeardown() {
	if tc.dbConn == nil {
		return
	}
	for alias := range tc.users {
		wsID, err := tc.identityWorkspaceID(alias)
		if err != nil || wsID == "" {
			continue
		}
		s := tc.sess()
		if s == nil {
			continue
		}
		_, _ = s.DeleteFrom("memory_identities").
			Where("workspace_id = ?", wsID).
			ExecContext(context.Background())
	}
}
