// Package e2e — database assertion step definitions.
// Each step queries the orchestrator's Postgres database to verify
// that API operations produced the expected data. Steps are skipped
// gracefully when no --database-dsn is provided.
package e2e

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/cucumber/godog"
	"github.com/gocraft/dbr/v2"
)

// registerDBSteps binds all Gherkin phrases that assert database state.
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

	// Agent-runtime assertions (polled up to 30s for async tool effects).
	sc.Step(`^the assistant should have remembered at least (\d+) notes? for subject "([^"]*)"$`, tc.dbAgentMemoryCountForSubject)
	sc.Step(`^the audit trail should include a "([^"]*)" tool call for subject "([^"]*)"$`, tc.dbMCPAuditLogForSubject)
	sc.Step(`^the assistant should have delegated at least (\d+) tasks? for subject "([^"]*)"$`, tc.dbAgentDelegationCountForSubject)
	sc.Step(`^the assistant should have at least (\d+) agent-to-agent messages? for subject "([^"]*)"$`, tc.dbAgentMessageCountForSubject)
}

// sess returns a dbr session for DB queries, or nil if not configured.
func (tc *testContext) sess() *dbr.Session {
	if tc.dbConn == nil {
		return nil
	}
	return tc.dbConn.NewSession(nil)
}

// resolveSubject maps a user alias (e.g. "alice") to its generated subject ID.
func (tc *testContext) resolveSubject(alias string) string {
	if user := tc.users[alias]; user != nil {
		return user.subject
	}
	return alias
}

// queryCount runs a raw COUNT query and returns the result.
func (tc *testContext) queryCount(query string, args ...any) (int, error) {
	s := tc.sess()
	if s == nil {
		return 0, nil
	}
	var count int
	row := s.QueryRowContext(context.Background(), query, args...)
	if err := row.Scan(&count); err != nil {
		return 0, fmt.Errorf("DB query failed: %w", err)
	}
	return count, nil
}

func (tc *testContext) dbHasUserWithSubject(alias string) error {
	if tc.dbConn == nil {
		return nil
	}
	subject := tc.resolveSubject(alias)
	count, err := tc.queryCount("SELECT COUNT(*) FROM users WHERE subject = $1", subject)
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("no user with subject %q in database", subject)
	}
	return nil
}

func (tc *testContext) dbWorkspaceCountForSubject(expected int, alias string) error {
	if tc.dbConn == nil {
		return nil
	}
	subject := tc.resolveSubject(alias)
	count, err := tc.queryCount(`
		SELECT COUNT(*) FROM workspaces
		JOIN users ON users.id = workspaces.user_id
		WHERE users.subject = $1`, subject)
	if err != nil {
		return err
	}
	if count != expected {
		return fmt.Errorf("expected %d workspace(s) for %q, got %d", expected, alias, count)
	}
	return nil
}

func (tc *testContext) dbAgentCountForSubject(expected int, alias string) error {
	if tc.dbConn == nil {
		return nil
	}
	subject := tc.resolveSubject(alias)
	count, err := tc.queryCount(`
		SELECT COUNT(*) FROM agents
		JOIN workspaces ON workspaces.id = agents.workspace_id
		JOIN users ON users.id = workspaces.user_id
		WHERE users.subject = $1`, subject)
	if err != nil {
		return err
	}
	if count != expected {
		return fmt.Errorf("expected %d agent(s) for %q, got %d", expected, alias, count)
	}
	return nil
}

func (tc *testContext) dbConversationCountForSubject(expected int, alias string) error {
	if tc.dbConn == nil {
		return nil
	}
	subject := tc.resolveSubject(alias)
	count, err := tc.queryCount(`
		SELECT COUNT(*) FROM conversations
		JOIN workspaces ON workspaces.id = conversations.workspace_id
		JOIN users ON users.id = workspaces.user_id
		WHERE users.subject = $1`, subject)
	if err != nil {
		return err
	}
	if count != expected {
		return fmt.Errorf("expected %d conversation(s) for %q, got %d", expected, alias, count)
	}
	return nil
}

func (tc *testContext) dbMessageCountForSubject(expected int, alias string) error {
	if tc.dbConn == nil {
		return nil
	}
	subject := tc.resolveSubject(alias)
	count, err := tc.queryCount(`
		SELECT COUNT(*) FROM messages
		JOIN conversations ON conversations.id = messages.conversation_id
		JOIN workspaces ON workspaces.id = conversations.workspace_id
		JOIN users ON users.id = workspaces.user_id
		WHERE users.subject = $1`, subject)
	if err != nil {
		return err
	}
	if count != expected {
		return fmt.Errorf("expected %d message(s) for %q, got %d", expected, alias, count)
	}
	return nil
}

func (tc *testContext) dbHasPushToken(token, alias string) error {
	if tc.dbConn == nil {
		return nil
	}
	subject := tc.resolveSubject(alias)
	count, err := tc.queryCount(`
		SELECT COUNT(*) FROM user_push_tokens
		JOIN users ON users.id = user_push_tokens.user_id
		WHERE users.subject = $1 AND user_push_tokens.push_token = $2`, subject, token)
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("no push token %q for subject %q", token, alias)
	}
	return nil
}

func (tc *testContext) dbUserHasNickname(alias, expected string) error {
	if tc.dbConn == nil {
		return nil
	}
	subject := tc.resolveSubject(alias)
	s := tc.sess()
	var got string
	row := s.QueryRowContext(context.Background(), "SELECT nickname FROM users WHERE subject = $1", subject)
	if err := row.Scan(&got); err != nil {
		return fmt.Errorf("DB query failed: %w", err)
	}
	if got != expected {
		return fmt.Errorf("expected nickname %q for %q, got %q", expected, alias, got)
	}
	return nil
}

func (tc *testContext) dbUserHasCountryCode(alias, expected string) error {
	if tc.dbConn == nil {
		return nil
	}
	subject := tc.resolveSubject(alias)
	s := tc.sess()
	var got sql.NullString
	row := s.QueryRowContext(context.Background(), "SELECT country_code FROM users WHERE subject = $1", subject)
	if err := row.Scan(&got); err != nil {
		return fmt.Errorf("DB query failed: %w", err)
	}
	if !got.Valid || got.String != expected {
		return fmt.Errorf("expected country_code %q for %q, got %v", expected, alias, got)
	}
	return nil
}

func (tc *testContext) dbUserHasDeletedAt(alias string) error {
	if tc.dbConn == nil {
		return nil
	}
	subject := tc.resolveSubject(alias)
	s := tc.sess()
	var hasDeletedAt bool
	row := s.QueryRowContext(context.Background(), "SELECT deleted_at IS NOT NULL FROM users WHERE subject = $1", subject)
	if err := row.Scan(&hasDeletedAt); err != nil {
		return fmt.Errorf("DB query failed: %w", err)
	}
	if !hasDeletedAt {
		return fmt.Errorf("expected deleted_at to be set for %q", alias)
	}
	return nil
}

func (tc *testContext) dbUserIsDeleted(alias, expected string) error {
	if tc.dbConn == nil {
		return nil
	}
	subject := tc.resolveSubject(alias)
	s := tc.sess()
	var isDeleted bool
	row := s.QueryRowContext(context.Background(), "SELECT deleted_at IS NOT NULL FROM users WHERE subject = $1", subject)
	if err := row.Scan(&isDeleted); err != nil {
		return fmt.Errorf("DB query failed: %w", err)
	}
	got := fmt.Sprintf("%v", isDeleted)
	if got != expected {
		return fmt.Errorf("expected is_deleted=%s for %q, got %s", expected, alias, got)
	}
	return nil
}

// --- Memory palace DB assertions (polled, async-tolerant) -----------

// dbAgentMemoryCountForSubject waits until the given subject's default
// workspace has at least `expected` memory_drawers rows. This is the
// assertion that backs "the assistant should have remembered at least
// N notes for subject X" in the Gherkin feature files; the drawer is
// written by the memory_add_drawer tool during agent turns.
func (tc *testContext) dbAgentMemoryCountForSubject(expected int, alias string) error {
	return tc.withDB(func(_ *dbr.Session) error {
		subject := tc.resolveSubject(alias)
		return tc.pollDefault(func() error {
			count, err := tc.queryCount(`
				SELECT COUNT(*) FROM memory_drawers
				JOIN workspaces ON workspaces.id = memory_drawers.workspace_id
				JOIN users ON users.id = workspaces.user_id
				WHERE users.subject = $1`, subject)
			if err != nil {
				return err
			}
			if count < expected {
				return fmt.Errorf("expected at least %d memory drawer(s) for %q, got %d", expected, alias, count)
			}
			return nil
		})
	})
}

func (tc *testContext) dbMCPAuditLogForSubject(toolName, alias string) error {
	return tc.withDB(func(_ *dbr.Session) error {
		subject := tc.resolveSubject(alias)
		return tc.pollDefault(func() error {
			count, err := tc.queryCount(`
				SELECT COUNT(*) FROM mcp_audit_logs
				JOIN users ON users.id::text = mcp_audit_logs.user_id
				WHERE users.subject = $1 AND mcp_audit_logs.tool_name = $2`, subject, toolName)
			if err != nil {
				return err
			}
			if count == 0 {
				return fmt.Errorf("no mcp_audit_logs row for tool %q and subject %q", toolName, alias)
			}
			return nil
		})
	})
}

func (tc *testContext) dbAgentDelegationCountForSubject(expected int, alias string) error {
	return tc.withDB(func(_ *dbr.Session) error {
		subject := tc.resolveSubject(alias)
		return tc.pollDefault(func() error {
			count, err := tc.queryCount(`
				SELECT COUNT(*) FROM agent_delegations
				JOIN conversations ON conversations.id = agent_delegations.conversation_id
				JOIN workspaces ON workspaces.id = conversations.workspace_id
				JOIN users ON users.id = workspaces.user_id
				WHERE users.subject = $1`, subject)
			if err != nil {
				return err
			}
			if count < expected {
				return fmt.Errorf("expected at least %d delegation(s) for %q, got %d", expected, alias, count)
			}
			return nil
		})
	})
}

func (tc *testContext) dbAgentMessageCountForSubject(expected int, alias string) error {
	return tc.withDB(func(_ *dbr.Session) error {
		subject := tc.resolveSubject(alias)
		return tc.pollDefault(func() error {
			count, err := tc.queryCount(`
				SELECT COUNT(*) FROM agent_messages
				JOIN conversations ON conversations.id = agent_messages.conversation_id
				JOIN workspaces ON workspaces.id = conversations.workspace_id
				JOIN users ON users.id = workspaces.user_id
				WHERE users.subject = $1`, subject)
			if err != nil {
				return err
			}
			if count < expected {
				return fmt.Errorf("expected at least %d agent-to-agent message(s) for %q, got %d", expected, alias, count)
			}
			return nil
		})
	})
}
