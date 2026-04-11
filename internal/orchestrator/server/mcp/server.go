// Package mcp implements the MCP (Model Context Protocol) server that
// agent runtime pods connect to for orchestrator-side tool execution.
package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/auditrepo"
	crawblhmac "github.com/Crawbl-AI/crawbl-backend/internal/pkg/hmac"
)

// NewHandler creates the HTTP handler for the MCP server at /mcp/v1.
func NewHandler(deps *Deps) http.Handler {
	server := sdkmcp.NewServer(
		&sdkmcp.Implementation{
			Name:    "crawbl-orchestrator",
			Version: mcpServerVersion,
		},
		&sdkmcp.ServerOptions{
			Instructions: strings.Join([]string{
				"Crawbl orchestrator MCP server.",
				"Tools for push notifications and user context.",
				"All data is scoped to the authenticated user — you cannot access other users' data.",
				"OAuth integration tools will be added in future phases.",
			}, " "),
			Logger: deps.Logger,
		},
	)

	registerTools(server, deps)

	server.AddReceivingMiddleware(auditMiddleware(deps))

	handler := sdkmcp.NewStreamableHTTPHandler(
		func(_ *http.Request) *sdkmcp.Server { return server },
		nil,
	)

	return withAuth(handler, deps)
}

// withAuth wraps the MCP handler with bearer token validation.
func withAuth(next http.Handler, deps *Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "missing authorization", http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")

		userID, workspaceID, err := crawblhmac.ValidateToken(deps.SigningKey, token)
		if err != nil {
			deps.Logger.Warn("mcp auth failed",
				slog.String("error", err.Error()),
				slog.String("remote_addr", r.RemoteAddr),
			)
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		sessionID := r.Header.Get("Mcp-Session-Id")
		ctx := contextWithIdentity(r.Context(), userID, workspaceID, sessionID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// auditMiddleware logs every MCP tool call via the audit service.
func auditMiddleware(deps *Deps) sdkmcp.Middleware {
	return func(next sdkmcp.MethodHandler) sdkmcp.MethodHandler {
		return func(ctx context.Context, method string, req sdkmcp.Request) (sdkmcp.Result, error) {
			if method != mcpToolCallMethod {
				return next(ctx, method, req)
			}

			start := time.Now()
			userID := userIDFromContext(ctx)
			workspaceID := workspaceIDFromContext(ctx)
			toolName, inputJSON := extractToolCallParams(req)

			result, err := next(ctx, method, req)
			duration := time.Since(start)

			outputJSON := extractResultJSON(result)
			apiCalls := apiCallsFromContext(ctx)

			auditCtx, auditCancel := context.WithTimeout(ctx, auditWriteTimeout)
			defer auditCancel()
			sess := deps.newSession()
			if logErr := deps.AuditService.WriteLog(auditCtx, sess, &auditrepo.AuditLogRow{
				UserID:      userID,
				WorkspaceID: workspaceID,
				SessionID:   sessionIDFromContext(ctx),
				ToolName:    toolName,
				Input:       inputJSON,
				Output:      outputJSON,
				APICalls:    apiCalls,
				Success:     err == nil,
				ErrorMsg:    errorString(err),
				DurationMs:  int(duration.Milliseconds()),
			}); logErr != nil {
				deps.Logger.Error("failed to write mcp audit log",
					slog.String("error", logErr.Error()),
					slog.String("tool", toolName),
					slog.String("user_id", userID),
				)
			}

			return result, err
		}
	}
}

func extractToolCallParams(req sdkmcp.Request) (toolName string, argsJSON string) {
	params, ok := req.GetParams().(*sdkmcp.CallToolParamsRaw)
	if !ok || params == nil {
		return "unknown", "{}"
	}
	return params.Name, safeTruncatedJSON(string(params.Arguments))
}

func extractResultJSON(result sdkmcp.Result) string {
	if result == nil {
		return "{}"
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "{}"
	}
	return safeTruncatedJSON(string(data))
}

// safeTruncatedJSON sanitises a raw JSON payload for storage in the
// mcp_audit_logs.input / output jsonb columns. It returns a valid JSON
// document even when the input was oversized, malformed, or empty —
// otherwise the Postgres jsonb column rejects the insert with
// "invalid input syntax for type json" and the whole audit write
// silently fails.
//
// Rules:
//  1. Empty or all-whitespace payload -> "{}" (the NOT NULL default).
//  2. Invalid UTF-8 sequences are stripped first.
//  3. If the payload exceeds auditMaxResponseBytes, replace it with a
//     structured placeholder rather than slicing mid-token and
//     appending "..." — string slicing corrupts JSON and is the
//     original source of the bug.
//  4. If the payload is not valid JSON for any other reason, wrap it
//     in a {"_raw": "<escaped>"} envelope so the row still lands.
func safeTruncatedJSON(raw string) string {
	s := strings.ToValidUTF8(raw, "")
	if strings.TrimSpace(s) == "" {
		return "{}"
	}
	if len(s) > auditMaxResponseBytes {
		placeholder := map[string]any{
			"_truncated":     true,
			"_original_size": len(s),
			"_limit":         auditMaxResponseBytes,
		}
		b, _ := json.Marshal(placeholder)
		return string(b)
	}
	if !json.Valid([]byte(s)) {
		b, _ := json.Marshal(map[string]string{"_raw": s})
		return string(b)
	}
	return s
}

func apiCallsFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyAPICalls).(*[]string)
	if v == nil || len(*v) == 0 {
		return ""
	}
	data, _ := json.Marshal(*v)
	return string(data)
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
