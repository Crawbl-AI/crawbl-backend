package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	crawblhmac "github.com/Crawbl-AI/crawbl-backend/internal/pkg/hmac"
)

// NewHandler creates the HTTP handler for the MCP server at /mcp/v1.
// It validates bearer tokens, injects user identity into the request context,
// and dispatches tool calls to the registered handlers.
func NewHandler(deps *Deps) http.Handler {
	server := sdkmcp.NewServer(
		&sdkmcp.Implementation{
			Name:    "crawbl-orchestrator",
			Version: "1.0.0",
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

	// Add audit logging middleware for ISO 27001 compliance.
	// Logs every tools/call invocation with input, output, duration, and identity.
	server.AddReceivingMiddleware(auditMiddleware(deps))

	handler := sdkmcp.NewStreamableHTTPHandler(
		func(_ *http.Request) *sdkmcp.Server { return server },
		nil,
	)

	return withAuth(handler, deps)
}

// withAuth wraps the MCP handler with bearer token validation.
// Extracts userID and workspaceID from the HMAC-signed token
// and injects them into the request context.
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

// auditMiddleware logs every MCP tool call to the database for ISO 27001 audit compliance.
func auditMiddleware(deps *Deps) sdkmcp.Middleware {
	return func(next sdkmcp.MethodHandler) sdkmcp.MethodHandler {
		return func(ctx context.Context, method string, req sdkmcp.Request) (sdkmcp.Result, error) {
			// Only audit tool calls, not initialize/list/etc.
			if method != "tools/call" {
				return next(ctx, method, req)
			}

			start := time.Now()
			userID := userIDFromContext(ctx)
			workspaceID := workspaceIDFromContext(ctx)

			// Extract tool name and input from the request params.
			toolName, inputJSON := extractToolCallParams(req)

			// Execute the actual tool handler.
			result, err := next(ctx, method, req)
			duration := time.Since(start)

			// Capture the output and any API calls made during tool execution.
			outputJSON := extractResultJSON(result)
			apiCalls := apiCallsFromContext(ctx)

			// Log the audit entry asynchronously to avoid slowing down the response.
			go func() {
				auditCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				logAudit(auditCtx, deps, auditEntry{
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
				})
			}()

			return result, err
		}
	}
}

// extractToolCallParams extracts the tool name and input JSON from an MCP request.
func extractToolCallParams(req sdkmcp.Request) (string, string) {
	params, ok := req.GetParams().(*sdkmcp.CallToolParamsRaw)
	if !ok || params == nil {
		return "unknown", "{}"
	}
	input := "{}"
	if len(params.Arguments) > 0 {
		input = string(params.Arguments)
	}
	return params.Name, input
}

// extractResultJSON serializes the MCP result for audit logging.
func extractResultJSON(result sdkmcp.Result) string {
	if result == nil {
		return "{}"
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "{}"
	}
	// Truncate to 2KB to avoid bloating audit logs with large responses.
	s := string(data)
	if len(s) > 2048 {
		return s[:2048] + "..."
	}
	return s
}

// apiCallsFromContext retrieves the recorded API calls from the request context.
// Tool handlers use RecordAPICall to log outgoing calls during execution.
func apiCallsFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyAPICalls).(*[]string)
	if v == nil || len(*v) == 0 {
		return ""
	}
	data, _ := json.Marshal(*v)
	return string(data)
}

// RecordAPICall appends an outgoing API call description to the context.
// Called by tool handlers to track what external calls they make.
// Format: "SERVICE:METHOD URL" e.g. "FCM:POST /v1/projects/crawbl-dev/messages:send"
func RecordAPICall(ctx context.Context, call string) {
	v, _ := ctx.Value(ctxKeyAPICalls).(*[]string)
	if v != nil {
		*v = append(*v, call)
	}
}

// logAudit inserts an audit log entry into the database.
func logAudit(ctx context.Context, deps *Deps, entry auditEntry) {
	sess := deps.newSession()
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
		ExecContext(ctx)
	if err != nil {
		deps.Logger.Error("failed to write mcp audit log",
			slog.String("error", err.Error()),
			slog.String("tool", entry.ToolName),
			slog.String("user_id", entry.UserID),
		)
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// registerTools adds all MCP tools to the server.
func registerTools(server *sdkmcp.Server, deps *Deps) {
	// Push notifications
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "send_push_notification",
		Description: "Send a push notification to the user's mobile device. Use for completed tasks, reminders, or important updates.",
	}, newPushHandler(deps))

	// User context
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "get_user_profile",
		Description: "Get the current user's profile: name, email, nickname, preferences.",
	}, newUserProfileHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "get_workspace_info",
		Description: "Get the current workspace name, agents, and creation date.",
	}, newWorkspaceInfoHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "list_conversations",
		Description: "List all conversations in the current workspace with titles, types, and timestamps.",
	}, newListConversationsHandler(deps))

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "search_past_messages",
		Description: "Search through past messages in a conversation by keyword. Use to recall what the user discussed before.",
	}, newSearchMessagesHandler(deps))
}
