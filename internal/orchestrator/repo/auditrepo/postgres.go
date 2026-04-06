// Package auditrepo provides persistence for MCP audit log entries.
package auditrepo

import (
	"context"
	"fmt"
	"time"

	"github.com/gocraft/dbr/v2"
)

// Repo persists MCP audit log entries.
type Repo interface {
	WriteLog(ctx context.Context, sess *dbr.Session, entry *AuditLogRow) error
}

// AuditLogRow maps to a row in the mcp_audit_logs table.
type AuditLogRow struct {
	UserID      string
	WorkspaceID string
	SessionID   string
	ToolName    string
	Input       string
	Output      string
	APICalls    string
	Success     bool
	ErrorMsg    string
	DurationMs  int
}

type postgres struct{}

// New returns a Postgres-backed audit log repository.
func New() Repo { return &postgres{} }

func (p *postgres) WriteLog(ctx context.Context, sess *dbr.Session, entry *AuditLogRow) error {
	_, err := sess.InsertInto("mcp_audit_logs").
		Pair("user_id", entry.UserID).
		Pair("workspace_id", entry.WorkspaceID).
		Pair("session_id", entry.SessionID).
		Pair("tool_name", entry.ToolName).
		Pair("input", entry.Input).
		Pair("output", entry.Output).
		Pair("api_calls", entry.APICalls).
		Pair("success", entry.Success).
		Pair("error_message", entry.ErrorMsg).
		Pair("duration_ms", entry.DurationMs).
		Pair("created_at", time.Now().UTC()).
		ExecContext(ctx)
	if err != nil {
		return fmt.Errorf("auditrepo: write log for tool %q: %w", entry.ToolName, err)
	}
	return nil
}
