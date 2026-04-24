// Package auditrepo provides persistence for MCP audit log entries.
package auditrepo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gocraft/dbr/v2"
)

// LogWriter persists MCP audit log entries.
type LogWriter interface {
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
func New() LogWriter { return &postgres{} }

func (p *postgres) WriteLog(ctx context.Context, sess *dbr.Session, entry *AuditLogRow) error {
	// Defensive JSON sanitisation: the mcp_audit_logs.input column is
	// NOT NULL jsonb with default '{}', while output and api_calls are
	// nullable jsonb. Writing a non-JSON string (truncated payload,
	// placeholder sentinel, etc.) would fail with "invalid input
	// syntax for type json" and silently drop the audit row. We normalise
	// at the repo boundary so upstream bugs never block the write.
	inputJSON := ensureJSON(entry.Input, "{}")
	outputJSON := ensureNullableJSON(entry.Output)
	apiCallsJSON := ensureNullableJSON(entry.APICalls)

	_, err := sess.InsertInto("mcp_audit_logs").
		Pair("user_id", entry.UserID).
		Pair("workspace_id", entry.WorkspaceID).
		Pair("session_id", entry.SessionID).
		Pair("tool_name", entry.ToolName).
		Pair("input", inputJSON).
		Pair("output", outputJSON).
		Pair("api_calls", apiCallsJSON).
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

// ensureJSON returns s when it parses as JSON, otherwise it returns
// fallback. Empty / whitespace inputs also map to fallback. This is
// the last-chance guard against truncated or malformed JSON corrupting
// the mcp_audit_logs.input column (which is NOT NULL).
func ensureJSON(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	if !json.Valid([]byte(s)) {
		// Fall back to an envelope so we preserve the broken payload
		// for later debugging rather than silently dropping it.
		b, _ := json.Marshal(map[string]string{"_raw": s})
		return string(b)
	}
	return s
}

// ensureNullableJSON returns a *string pointing at a valid JSON
// document, or nil to write SQL NULL. Empty / whitespace inputs map
// to NULL rather than an empty string, because jsonb rejects "".
func ensureNullableJSON(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	if !json.Valid([]byte(s)) {
		b, _ := json.Marshal(map[string]string{"_raw": s})
		v := string(b)
		return v
	}
	return s
}
