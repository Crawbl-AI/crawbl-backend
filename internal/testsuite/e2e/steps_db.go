package e2e

import (
	"fmt"

	"github.com/cucumber/godog"
)

func registerDBSteps(sc *godog.ScenarioContext, tc *testContext) {
	sc.Step(`^the database should have a user with subject "([^"]*)"$`, tc.dbHasUserWithSubject)
	sc.Step(`^the database should have (\d+) workspace(?:s)? for subject "([^"]*)"$`, tc.dbWorkspaceCountForSubject)
	sc.Step(`^the database should have (\d+) agent(?:s)? for subject "([^"]*)"$`, tc.dbAgentCountForSubject)
	sc.Step(`^the database should have (\d+) conversation(?:s)? for subject "([^"]*)"$`, tc.dbConversationCountForSubject)
	sc.Step(`^the database should have (\d+) message(?:s)? in conversation for subject "([^"]*)"$`, tc.dbMessageCountForSubject)
	sc.Step(`^the database should have push token "([^"]*)" for subject "([^"]*)"$`, tc.dbHasPushToken)
	sc.Step(`^the database user "([^"]*)" should have nickname "([^"]*)"$`, tc.dbUserHasNickname)
	sc.Step(`^the database user "([^"]*)" should have country_code "([^"]*)"$`, tc.dbUserHasCountryCode)
	sc.Step(`^the database user "([^"]*)" should have deleted_at set$`, tc.dbUserHasDeletedAt)
	sc.Step(`^the database user "([^"]*)" should have is_deleted "([^"]*)"$`, tc.dbUserIsDeleted)
}

func (tc *testContext) resolveSubject(alias string) string {
	user := tc.users[alias]
	if user != nil {
		return user.subject
	}
	return alias
}

func (tc *testContext) dbHasUserWithSubject(alias string) error {
	if tc.db == nil {
		return nil // skip DB assertions if no connection
	}
	subject := tc.resolveSubject(alias)
	var count int
	err := tc.db.QueryRow("SELECT COUNT(*) FROM users WHERE subject = $1", subject).Scan(&count)
	if err != nil {
		return fmt.Errorf("DB query failed: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("no user with subject %q in database", subject)
	}
	return nil
}

func (tc *testContext) dbWorkspaceCountForSubject(expected int, alias string) error {
	if tc.db == nil {
		return nil
	}
	subject := tc.resolveSubject(alias)
	var count int
	err := tc.db.QueryRow(`
		SELECT COUNT(*) FROM workspaces w
		JOIN users u ON u.id = w.user_id
		WHERE u.subject = $1`, subject).Scan(&count)
	if err != nil {
		return fmt.Errorf("DB query failed: %w", err)
	}
	if count != expected {
		return fmt.Errorf("expected %d workspaces for %q, got %d", expected, alias, count)
	}
	return nil
}

func (tc *testContext) dbAgentCountForSubject(expected int, alias string) error {
	if tc.db == nil {
		return nil
	}
	subject := tc.resolveSubject(alias)
	var count int
	err := tc.db.QueryRow(`
		SELECT COUNT(*) FROM agents a
		JOIN workspaces w ON w.id = a.workspace_id
		JOIN users u ON u.id = w.user_id
		WHERE u.subject = $1`, subject).Scan(&count)
	if err != nil {
		return fmt.Errorf("DB query failed: %w", err)
	}
	if count != expected {
		return fmt.Errorf("expected %d agents for %q, got %d", expected, alias, count)
	}
	return nil
}

func (tc *testContext) dbConversationCountForSubject(expected int, alias string) error {
	if tc.db == nil {
		return nil
	}
	subject := tc.resolveSubject(alias)
	var count int
	err := tc.db.QueryRow(`
		SELECT COUNT(*) FROM conversations c
		JOIN workspaces w ON w.id = c.workspace_id
		JOIN users u ON u.id = w.user_id
		WHERE u.subject = $1`, subject).Scan(&count)
	if err != nil {
		return fmt.Errorf("DB query failed: %w", err)
	}
	if count != expected {
		return fmt.Errorf("expected %d conversations for %q, got %d", expected, alias, count)
	}
	return nil
}

func (tc *testContext) dbMessageCountForSubject(expected int, alias string) error {
	if tc.db == nil {
		return nil
	}
	subject := tc.resolveSubject(alias)
	var count int
	err := tc.db.QueryRow(`
		SELECT COUNT(*) FROM messages m
		JOIN conversations c ON c.id = m.conversation_id
		JOIN workspaces w ON w.id = c.workspace_id
		JOIN users u ON u.id = w.user_id
		WHERE u.subject = $1`, subject).Scan(&count)
	if err != nil {
		return fmt.Errorf("DB query failed: %w", err)
	}
	if count != expected {
		return fmt.Errorf("expected %d messages for %q, got %d", expected, alias, count)
	}
	return nil
}

func (tc *testContext) dbHasPushToken(token, alias string) error {
	if tc.db == nil {
		return nil
	}
	subject := tc.resolveSubject(alias)
	var count int
	err := tc.db.QueryRow(`
		SELECT COUNT(*) FROM user_push_tokens pt
		JOIN users u ON u.id = pt.user_id
		WHERE u.subject = $1 AND pt.push_token = $2`, subject, token).Scan(&count)
	if err != nil {
		return fmt.Errorf("DB query failed: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("no push token %q for subject %q", token, alias)
	}
	return nil
}

func (tc *testContext) dbUserHasNickname(alias, expected string) error {
	if tc.db == nil {
		return nil
	}
	subject := tc.resolveSubject(alias)
	var got string
	err := tc.db.QueryRow("SELECT nickname FROM users WHERE subject = $1", subject).Scan(&got)
	if err != nil {
		return fmt.Errorf("DB query failed: %w", err)
	}
	if got != expected {
		return fmt.Errorf("expected nickname %q for %q, got %q", expected, alias, got)
	}
	return nil
}

func (tc *testContext) dbUserHasCountryCode(alias, expected string) error {
	if tc.db == nil {
		return nil
	}
	subject := tc.resolveSubject(alias)
	var got *string
	err := tc.db.QueryRow("SELECT country_code FROM users WHERE subject = $1", subject).Scan(&got)
	if err != nil {
		return fmt.Errorf("DB query failed: %w", err)
	}
	if got == nil || *got != expected {
		return fmt.Errorf("expected country_code %q for %q, got %v", expected, alias, got)
	}
	return nil
}

func (tc *testContext) dbUserHasDeletedAt(alias string) error {
	if tc.db == nil {
		return nil
	}
	subject := tc.resolveSubject(alias)
	var hasDeletedAt bool
	err := tc.db.QueryRow("SELECT deleted_at IS NOT NULL FROM users WHERE subject = $1", subject).Scan(&hasDeletedAt)
	if err != nil {
		return fmt.Errorf("DB query failed: %w", err)
	}
	if !hasDeletedAt {
		return fmt.Errorf("expected deleted_at to be set for %q", alias)
	}
	return nil
}

func (tc *testContext) dbUserIsDeleted(alias, expected string) error {
	if tc.db == nil {
		return nil
	}
	subject := tc.resolveSubject(alias)
	var isDeleted bool
	err := tc.db.QueryRow("SELECT deleted_at IS NOT NULL FROM users WHERE subject = $1", subject).Scan(&isDeleted)
	if err != nil {
		return fmt.Errorf("DB query failed: %w", err)
	}
	got := fmt.Sprintf("%v", isDeleted)
	if got != expected {
		return fmt.Errorf("expected is_deleted=%s for %q, got %s", expected, alias, got)
	}
	return nil
}
