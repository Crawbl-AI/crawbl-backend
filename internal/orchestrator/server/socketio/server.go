// Package socketio sets up the Socket.IO server for real-time WebSocket communication.
//
// It handles:
//   - Socket.IO server creation with optional Redis adapter for cross-pod fan-out
//   - Authentication via Envoy-forwarded claim headers (X-Firebase-UID/Email/Name)
//   - Workspace room join/leave on connect/disconnect
package socketio

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
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
)

// NewServer creates and configures a Socket.IO server with authentication
// middleware, workspace room management, and Redis adapter for cross-pod event fan-out.
//
// The returned server is ready to be mounted as an http.Handler via Handler.
func NewServer(cfg *Config) *socket.Server {
	if cfg == nil {
		panic("socketio config is required")
	}
	if cfg.Logger == nil {
		panic("socketio logger is required")
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
	registerAuthMiddleware(nsp, cfg.Logger)
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

// registerAuthMiddleware adds namespace-level middleware that reads the
// gateway-verified identity from forwarded headers. Envoy SecurityPolicy
// verifies the Firebase JWT and sets X-Firebase-UID/Email/Name headers.
//
// On success the authenticated Principal is stored in socket.Data() so
// downstream handlers can access it. On failure the connection is rejected.
func registerAuthMiddleware(nsp socket.Namespace, logger *slog.Logger) {
	nsp.Use(func(s *socket.Socket, next func(*socket.ExtendedError)) {
		h := s.Handshake()
		uid := headerFromHandshake(h, httpserver.XFirebaseUIDHeader)
		if uid == "" {
			logger.Warn("socketio auth: missing X-Firebase-UID",
				"socket_id", string(s.Id()),
			)
			next(socket.NewExtendedError("unauthorized", "missing authentication"))
			return
		}

		principal := &orchestrator.Principal{
			Subject: uid,
			Email:   headerFromHandshake(h, httpserver.XFirebaseEmailHeader),
			Name:    headerFromHandshake(h, httpserver.XFirebaseNameHeader),
		}

		s.SetData(principal)

		logger.Info("socketio auth: authenticated",
			"socket_id", string(s.Id()),
			"subject", principal.Subject,
		)

		next(nil)
	})
}

// headerFromHandshake extracts a trimmed header value from the Socket.IO handshake.
func headerFromHandshake(h *socket.Handshake, key string) string {
	if h == nil || h.Headers == nil {
		return ""
	}
	return strings.TrimSpace(http.Header(h.Headers).Get(key))
}

// registerConnectionHandler sets up the "connection" event on the namespace.
// For each authenticated socket it registers workspace.subscribe/unsubscribe
// event handlers for dynamic room management and a disconnect handler for cleanup.
func registerConnectionHandler(nsp socket.Namespace, logger *slog.Logger) {
	nsp.On("connection", func(args ...any) {
		s, ok := args[0].(*socket.Socket)
		if !ok {
			return
		}

		subject := principalSubjectFromSocket(s)

		logger.Info("socketio: client connected",
			"socket_id", string(s.Id()),
			"subject", subject,
		)

		// workspace.subscribe — join rooms and acknowledge.
		s.On(eventWorkspaceSubscribe, func(args ...any) {
			ids := parseWorkspaceIDs(args)
			if len(ids) == 0 {
				return
			}
			for _, id := range ids {
				s.Join(socket.Room(workspaceRoomPrefix + id))
			}
			logger.Info("socketio: workspace subscribe",
				"socket_id", string(s.Id()),
				"subject", subject,
				"workspace_ids", ids,
			)
			s.Emit(eventWorkspaceSubscribed, workspaceSubscribePayload{WorkspaceIDs: ids})
		})

		// workspace.unsubscribe — leave rooms.
		s.On(eventWorkspaceUnsubscribe, func(args ...any) {
			ids := parseWorkspaceIDs(args)
			if len(ids) == 0 {
				return
			}
			for _, id := range ids {
				s.Leave(socket.Room(workspaceRoomPrefix + id))
			}
			logger.Info("socketio: workspace unsubscribe",
				"socket_id", string(s.Id()),
				"subject", subject,
				"workspace_ids", ids,
			)
		})

		s.On("disconnect", func(args ...any) {
			reason := ""
			if len(args) > 0 {
				if r, ok := args[0].(string); ok {
					reason = r
				}
			}
			logger.Info("socketio: client disconnected",
				"socket_id", string(s.Id()),
				"subject", subject,
				"reason", reason,
			)
		})
	})
}

// parseWorkspaceIDs extracts workspace IDs from a workspace.subscribe/unsubscribe event.
func parseWorkspaceIDs(args []any) []string {
	if len(args) == 0 {
		return nil
	}
	data, ok := args[0].(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := data["workspace_ids"]
	if !ok {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	ids := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			ids = append(ids, strings.TrimSpace(s))
		}
	}
	return ids
}

// Handler returns an http.Handler that serves the Socket.IO engine.
// Mount this on your HTTP server or router at the appropriate path (typically "/socket.io/").
func Handler(io *socket.Server) http.Handler {
	return io.ServeHandler(nil)
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
