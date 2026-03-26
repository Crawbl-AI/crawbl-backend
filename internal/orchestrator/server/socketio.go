// Package server — socketio.go sets up the Socket.IO server for real-time WebSocket communication.
//
// It handles:
//   - Socket.IO server creation with optional Redis adapter for cross-pod fan-out
//   - Authentication on connection using the same IdentityVerifier as REST endpoints
//   - Workspace room join/leave on connect/disconnect
//   - SocketIOBroadcaster that implements realtime.Broadcaster
package server

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/redis/go-redis/v9"

	"github.com/zishang520/engine.io/v2/types"
	redisadapter "github.com/zishang520/socket.io-go-redis/adapter"
	redistypes "github.com/zishang520/socket.io-go-redis/types"
	"github.com/zishang520/socket.io/v2/socket"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
)

// socketNamespace is the Socket.IO namespace path that the mobile client connects to.
const socketNamespace = "/v1"

// workspaceRoomPrefix is prepended to workspace IDs to form room names.
const workspaceRoomPrefix = "workspace:"

// SocketIOConfig holds the dependencies for creating a Socket.IO server.
type SocketIOConfig struct {
	// Logger provides structured logging for Socket.IO operations.
	Logger *slog.Logger

	// IdentityVerifier validates tokens and extracts principal information.
	// This is the same verifier used by the REST auth middleware.
	IdentityVerifier orchestrator.IdentityVerifier

	// RedisClient is an optional Redis client for the pub/sub adapter.
	// When nil, the server falls back to the default in-memory adapter.
	RedisClient *redis.Client
}

// NewSocketIOServer creates and configures a Socket.IO server with authentication
// middleware, workspace room management, and an optional Redis adapter for
// cross-pod event fan-out.
//
// The returned server is ready to be mounted as an http.Handler via SocketIOHandler.
func NewSocketIOServer(cfg *SocketIOConfig) *socket.Server {
	if cfg == nil {
		panic("socketio config is required")
	}
	if cfg.Logger == nil {
		panic("socketio logger is required")
	}
	if cfg.IdentityVerifier == nil {
		panic("socketio identity verifier is required")
	}

	opts := socket.DefaultServerOptions()
	opts.SetCors(&types.Cors{
		Origin:      "*",
		Credentials: true,
	})
	// Only allow websocket transport — mobile client sets setTransports(['websocket']).
	opts.SetTransports(types.NewSet("websocket"))

	io := socket.NewServer(nil, opts)

	// Configure Redis adapter for multi-pod deployments when a Redis client is available.
	configureRedisAdapter(io, cfg.RedisClient, cfg.Logger)

	// Set up the /v1 namespace with auth middleware and connection handling.
	nsp := io.Of(socketNamespace, nil)
	registerAuthMiddleware(nsp, cfg.IdentityVerifier, cfg.Logger)
	registerConnectionHandler(nsp, cfg.Logger)

	return io
}

// configureRedisAdapter attaches the Redis pub/sub adapter to the Socket.IO server.
// If redisClient is nil the default in-memory adapter is kept, which is fine for
// single-pod development but will not fan out events across pods.
func configureRedisAdapter(io *socket.Server, redisClient *redis.Client, logger *slog.Logger) {
	if redisClient == nil {
		logger.Info("socketio: no redis client provided, using in-memory adapter")
		return
	}

	rc := redistypes.NewRedisClient(context.Background(), redisClient)
	builder := &redisadapter.RedisAdapterBuilder{
		Redis: rc,
	}
	io.SetAdapter(builder)

	logger.Info("socketio: redis adapter configured for cross-pod fan-out")
}

// registerAuthMiddleware adds namespace-level middleware that authenticates every
// incoming socket connection using the same IdentityVerifier as the REST endpoints.
//
// Token extraction follows the mobile client contract:
//  1. X-Token header (primary mobile path)
//  2. Authorization: Bearer <token> (dev/tooling compatibility)
//
// On success the authenticated Principal is stored in socket.Data() so downstream
// handlers can access it. On failure the connection is rejected.
func registerAuthMiddleware(nsp socket.Namespace, verifier orchestrator.IdentityVerifier, logger *slog.Logger) {
	nsp.Use(func(s *socket.Socket, next func(*socket.ExtendedError)) {
		token, source := extractTokenFromHandshake(s.Handshake())
		if token == "" {
			logger.Warn("socketio auth: missing token",
				"socket_id", string(s.Id()),
			)
			next(socket.NewExtendedError("unauthorized", "missing authentication token"))
			return
		}

		principal, err := verifier.Verify(context.Background(), token)
		if err != nil {
			logger.Warn("socketio auth: verification failed",
				"socket_id", string(s.Id()),
				"token_source", source,
				"error", err.Error(),
			)
			next(socket.NewExtendedError("unauthorized", "invalid token"))
			return
		}

		// Store the principal so the connection handler can read it.
		s.SetData(principal)

		logger.Info("socketio auth: authenticated",
			"socket_id", string(s.Id()),
			"subject", principal.Subject,
			"token_source", source,
		)

		next(nil)
	})
}

// registerConnectionHandler sets up the "connection" event on the namespace.
// For each authenticated socket it:
//   - Reads the workspaceId query parameter
//   - Joins the socket to the workspace room
//   - Registers a disconnect handler for cleanup logging
func registerConnectionHandler(nsp socket.Namespace, logger *slog.Logger) {
	nsp.On("connection", func(args ...any) {
		s, ok := args[0].(*socket.Socket)
		if !ok {
			return
		}

		workspaceID := extractWorkspaceID(s.Handshake())
		if workspaceID == "" {
			logger.Warn("socketio: missing workspaceId query param, disconnecting",
				"socket_id", string(s.Id()),
			)
			s.Disconnect(true)
			return
		}

		// Join the workspace-scoped room for targeted broadcasts.
		room := socket.Room(workspaceRoomPrefix + workspaceID)
		s.Join(room)

		// Extract subject for logging, if available.
		subject := principalSubjectFromSocket(s)

		logger.Info("socketio: client connected",
			"socket_id", string(s.Id()),
			"workspace_id", workspaceID,
			"room", string(room),
			"subject", subject,
		)

		s.On("disconnect", func(args ...any) {
			reason := ""
			if len(args) > 0 {
				if r, ok := args[0].(string); ok {
					reason = r
				}
			}
			logger.Info("socketio: client disconnected",
				"socket_id", string(s.Id()),
				"workspace_id", workspaceID,
				"subject", subject,
				"reason", reason,
			)
		})
	})
}

// SocketIOHandler returns an http.Handler that serves the Socket.IO engine.
// Mount this on your HTTP server or router at the appropriate path (typically "/socket.io/").
func SocketIOHandler(io *socket.Server) http.Handler {
	return io.ServeHandler(nil)
}

// ---------------------------------------------------------------------------
// SocketIOBroadcaster — implements realtime.Broadcaster
// ---------------------------------------------------------------------------

// SocketIOBroadcaster emits real-time events to connected clients via Socket.IO.
// It broadcasts to workspace-scoped rooms so only clients subscribed to a given
// workspace receive the events. When a Redis adapter is configured, events are
// automatically fanned out across all pods.
type SocketIOBroadcaster struct {
	io     *socket.Server
	logger *slog.Logger
}

// NewSocketIOBroadcaster creates a Broadcaster backed by the given Socket.IO server.
func NewSocketIOBroadcaster(io *socket.Server, logger *slog.Logger) *SocketIOBroadcaster {
	return &SocketIOBroadcaster{
		io:     io,
		logger: logger,
	}
}

// Compile-time interface satisfaction check.
var _ realtime.Broadcaster = (*SocketIOBroadcaster)(nil)

// EmitToWorkspace sends a named event with payload to all clients in a workspace room.
func (b *SocketIOBroadcaster) EmitToWorkspace(_ context.Context, workspaceID string, event string, data any) {
	room := socket.Room(workspaceRoomPrefix + workspaceID)
	if err := b.io.Of(socketNamespace, nil).To(room).Emit(event, data); err != nil {
		b.logger.Error("socketio broadcast: emit failed",
			"event", event,
			"workspace_id", workspaceID,
			"error", err.Error(),
		)
	}
}

// EmitMessageNew emits a message.new event for a newly created message.
func (b *SocketIOBroadcaster) EmitMessageNew(ctx context.Context, workspaceID string, data any) {
	b.EmitToWorkspace(ctx, workspaceID, realtime.EventMessageNew, realtime.MessageEventPayload{
		Event: realtime.EventMessageNew,
		Data:  data,
	})
}

// EmitMessageUpdated emits a message.updated event for a modified message.
func (b *SocketIOBroadcaster) EmitMessageUpdated(ctx context.Context, workspaceID string, data any) {
	b.EmitToWorkspace(ctx, workspaceID, realtime.EventMessageUpdated, realtime.MessageEventPayload{
		Event: realtime.EventMessageUpdated,
		Data:  data,
	})
}

// EmitAgentTyping emits an agent.typing event indicating whether an agent is typing.
func (b *SocketIOBroadcaster) EmitAgentTyping(ctx context.Context, workspaceID string, conversationID, agentID string, isTyping bool) {
	b.EmitToWorkspace(ctx, workspaceID, realtime.EventAgentTyping, realtime.AgentTypingPayload{
		Event: realtime.EventAgentTyping,
		Data: realtime.AgentTypingData{
			ConversationID: conversationID,
			AgentID:        agentID,
			IsTyping:       isTyping,
		},
	})
}

// EmitAgentStatus emits an agent.status event when an agent's availability changes.
func (b *SocketIOBroadcaster) EmitAgentStatus(ctx context.Context, workspaceID string, agentID string, status string) {
	b.EmitToWorkspace(ctx, workspaceID, realtime.EventAgentStatus, realtime.AgentStatusPayload{
		Event: realtime.EventAgentStatus,
		Data: realtime.AgentStatusData{
			AgentID: agentID,
			Status:  status,
		},
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// extractTokenFromHandshake extracts the authentication token from the Socket.IO
// handshake headers. It mirrors the REST middleware priority:
//  1. X-Token header
//  2. Authorization: Bearer <token>
func extractTokenFromHandshake(h *socket.Handshake) (token, source string) {
	if h == nil {
		return "", "unknown"
	}

	// Check X-Token header first (primary mobile path).
	if values, ok := h.Headers["x-token"]; ok && len(values) > 0 {
		if t := strings.TrimSpace(values[0]); t != "" {
			return t, "x-token"
		}
	}
	// Headers may be title-cased depending on the transport.
	if values, ok := h.Headers["X-Token"]; ok && len(values) > 0 {
		if t := strings.TrimSpace(values[0]); t != "" {
			return t, "x-token"
		}
	}

	// Fallback to Authorization: Bearer (dev/tooling).
	for _, key := range []string{"authorization", "Authorization"} {
		if values, ok := h.Headers[key]; ok && len(values) > 0 {
			header := strings.TrimSpace(values[0])
			if strings.HasPrefix(header, "Bearer ") {
				if t := strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")); t != "" {
					return t, "authorization"
				}
			}
		}
	}

	return "", "unknown"
}

// extractWorkspaceID reads the workspaceId query parameter from the handshake.
// The mobile client sends it as: io('$baseUrl/v1', { query: { workspaceId: id } }).
func extractWorkspaceID(h *socket.Handshake) string {
	if h == nil || h.Query == nil {
		return ""
	}
	if values, ok := h.Query["workspaceId"]; ok && len(values) > 0 {
		return strings.TrimSpace(values[0])
	}
	return ""
}

// principalSubjectFromSocket extracts the principal subject stored in socket.Data()
// by the auth middleware. Returns empty string if unavailable.
func principalSubjectFromSocket(s *socket.Socket) string {
	if s == nil {
		return ""
	}
	p, ok := s.Data().(*orchestrator.Principal)
	if !ok || p == nil {
		return ""
	}
	return p.Subject
}
