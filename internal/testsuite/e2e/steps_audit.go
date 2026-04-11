// Package e2e — audit step definitions.
// Assertions over the assistant's tool-use records written by the
// orchestrator's audit middleware on every MCP tool call.
package e2e

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/cucumber/godog"
)

// registerAuditSteps binds all Gherkin phrases that assert tool-use audit state.
func registerAuditSteps(sc *godog.ScenarioContext, tc *testContext) {
	sc.Step(`^the assistant's save-memory tool use should be recorded as successful within (\d+) seconds?$`, tc.auditToolRecordedSuccessWithin)
	sc.Step(`^the save-memory tool use should have taken a measurable amount of time$`, tc.auditToolDurationNonZero)
	sc.Step(`^the assistant's recent tool uses should belong to the same session$`, tc.auditToolSharedSession)
}

// auditToolRecordedSuccessWithin polls until a successful memory_add_drawer
// audit record exists for the current user, timing out after the given seconds.
func (tc *testContext) auditToolRecordedSuccessWithin(seconds int) error {
	if tc.dbConn == nil {
		return nil
	}
	subject := tc.resolveSubject("primary")
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds)*time.Second)
	defer cancel()
	return pollUntil(ctx, func() error {
		s := tc.sess()
		if s == nil {
			return nil
		}
		var count int
		row := s.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM mcp_audit_logs
			 JOIN users ON users.id::text = mcp_audit_logs.user_id
			 WHERE users.subject = $1
			   AND mcp_audit_logs.tool_name = 'memory_add_drawer'
			   AND mcp_audit_logs.success = true`,
			subject,
		)
		if err := row.Scan(&count); err != nil {
			return fmt.Errorf("DB query failed: %w", err)
		}
		if count == 0 {
			return fmt.Errorf("no successful save-memory tool use record found for %q", subject)
		}
		return nil
	})
}

// auditToolDurationNonZero asserts that the most recent memory_add_drawer
// audit record for the current user has a non-zero duration.
func (tc *testContext) auditToolDurationNonZero() error {
	if tc.dbConn == nil {
		return nil
	}
	subject := tc.resolveSubject("primary")
	s := tc.sess()
	if s == nil {
		return nil
	}
	var durationMs sql.NullInt64
	row := s.QueryRowContext(context.Background(),
		`SELECT mcp_audit_logs.duration_ms
		 FROM mcp_audit_logs
		 JOIN users ON users.id::text = mcp_audit_logs.user_id
		 WHERE users.subject = $1
		   AND mcp_audit_logs.tool_name = 'memory_add_drawer'
		   AND mcp_audit_logs.success = true
		 ORDER BY mcp_audit_logs.created_at DESC
		 LIMIT 1`,
		subject,
	)
	if err := row.Scan(&durationMs); err != nil {
		return fmt.Errorf("DB query failed: %w", err)
	}
	if !durationMs.Valid || durationMs.Int64 == 0 {
		return fmt.Errorf("save-memory tool use record has zero or null duration for %q", subject)
	}
	return nil
}

// auditToolSharedSession asserts that all recent memory_add_drawer tool uses
// for the current user share the same session identifier.
func (tc *testContext) auditToolSharedSession() error {
	if tc.dbConn == nil {
		return nil
	}
	subject := tc.resolveSubject("primary")
	ctx, cancel := context.WithTimeout(context.Background(), asyncAssertTimeout)
	defer cancel()
	return pollUntil(ctx, func() error {
		s := tc.sess()
		if s == nil {
			return nil
		}
		var distinctSessions int
		row := s.QueryRowContext(context.Background(),
			`SELECT COUNT(DISTINCT mcp_audit_logs.session_id)
			 FROM mcp_audit_logs
			 JOIN users ON users.id::text = mcp_audit_logs.user_id
			 WHERE users.subject = $1
			   AND mcp_audit_logs.tool_name = 'memory_add_drawer'
			   AND mcp_audit_logs.created_at > NOW() - INTERVAL '1 minute'`,
			subject,
		)
		if err := row.Scan(&distinctSessions); err != nil {
			return fmt.Errorf("DB query failed: %w", err)
		}
		if distinctSessions > 1 {
			return fmt.Errorf("expected all recent tool uses to share one session, found %d distinct sessions for %q", distinctSessions, subject)
		}
		return nil
	})
}
