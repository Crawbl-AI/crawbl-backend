package mcp

import (
	"log/slog"
	"net/http"
	"strings"

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

		ctx := contextWithIdentity(r.Context(), userID, workspaceID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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
