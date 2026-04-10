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
	"regexp"
	"strings"

	"github.com/gocraft/dbr/v2"
	"github.com/redis/go-redis/v9"

	"github.com/zishang520/engine.io/v2/types"
	redisadapter "github.com/zishang520/socket.io-go-redis/adapter"
	redistypes "github.com/zishang520/socket.io-go-redis/types"
	"github.com/zishang520/socket.io/v2/socket"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
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
		// Explicit origin allowlist — wildcard + Credentials is rejected by browsers.
		// Supports: local dev (any localhost port), all *.crawbl.com subdomains over
		// HTTPS, and the Crawbl mobile app custom scheme.
		Origin: []any{
			regexp.MustCompile(`^http://localhost(:\d+)?$`),
			regexp.MustCompile(`^https://[a-zA-Z0-9-]+\.crawbl\.com$`),
			"crawbl://app",
		},
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
	registerConnectionHandler(nsp, cfg.Logger, cfg.DB, cfg.WorkspaceRepo, cfg.AuthService)

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
// For each authenticated socket it:
//   - upgrades socket.Data() from *Principal to *socketData (adding a socketSession)
//   - registers workspace.subscribe/unsubscribe event handlers for dynamic room management
//   - registers a single disconnect handler for cleanup and in-flight dispatch cancellation
//
// db, workspaceRepo, and authService are used to verify ownership before joining a
// workspace room. authService resolves the Firebase subject to an internal user.ID so
// the workspace ownership query uses the correct PK column. When db or workspaceRepo is
// nil the ownership check is skipped (development / test only).
func registerConnectionHandler(nsp socket.Namespace, logger *slog.Logger, db *dbr.Connection, workspaceRepo workspaceOwnerChecker, authService orchestratorservice.AuthService) {
	_ = nsp.On("connection", func(args ...any) {
		s, ok := args[0].(*socket.Socket)
		if !ok {
			return
		}

		// Auth middleware stored a *Principal in Data(). Wrap it alongside a fresh
		// socketSession so downstream handlers can access both via *socketData.
		principal, _ := s.Data().(*orchestrator.Principal)
		sd := &socketData{
			Principal: principal,
			Session:   &socketSession{},
		}
		s.SetData(sd)

		subject := principalSubjectFromSocket(s)

		logger.Info("socketio: client connected",
			"socket_id", string(s.Id()),
			"subject", subject,
		)

		// workspace.subscribe — verify ownership then join rooms and acknowledge.
		_ = s.On(eventWorkspaceSubscribe, func(args ...any) {
			ids := parseWorkspaceIDs(args)
			if len(ids) == 0 {
				return
			}

			// Determine the authenticated user's subject. Reject the subscribe
			// entirely when no principal is present (should not happen after auth
			// middleware, but guard defensively).
			if sd.Principal == nil || strings.TrimSpace(sd.Principal.Subject) == "" {
				logger.Warn("socketio: workspace subscribe rejected — no principal",
					"socket_id", string(s.Id()),
				)
				return
			}
			userSubject := sd.Principal.Subject

			// When DB + repo are available, verify each requested workspace is
			// owned by the authenticated user before joining its room.
			// IDs that fail the check are silently dropped — we do not reveal
			// whether the workspace exists to prevent enumeration attacks.
			authorised := ids
			if db != nil && workspaceRepo != nil {
				authorised = make([]string, 0, len(ids))
				sess := db.NewSession(nil)

				// Resolve Firebase subject → internal user.ID. The workspace repo
				// queries by the internal PK (user_id column), not the subject.
				// On failure, drop all requested IDs and bail early.
				var userID string
				if authService != nil {
					user, mErr := authService.GetBySubject(context.Background(), &orchestratorservice.GetUserBySubjectOpts{
						Sess:    sess,
						Subject: userSubject,
					})
					if mErr != nil {
						logger.Warn("socketio: workspace subscribe — subject resolution failed, dropping all ids",
							"socket_id", string(s.Id()),
							"subject", userSubject,
							"error", mErr.Error(),
						)
						return
					}
					userID = user.ID
				} else {
					// authService unavailable (dev/test): fall back to subject as-is.
					userID = userSubject
				}

				for _, id := range ids {
					_, mErr := workspaceRepo.GetByID(context.Background(), sess, userID, id)
					if mErr != nil {
						logger.Warn("socketio: workspace subscribe ownership check failed — dropping id",
							"socket_id", string(s.Id()),
							"subject", userSubject,
							"workspace_id", id,
						)
						continue
					}
					authorised = append(authorised, id)
				}
			}

			if len(authorised) == 0 {
				return
			}

			for _, id := range authorised {
				s.Join(socket.Room(workspaceRoomPrefix + id))
			}
			logger.Info("socketio: workspace subscribe",
				"socket_id", string(s.Id()),
				"subject", subject,
				"workspace_ids", authorised,
			)
			_ = s.Emit(eventWorkspaceSubscribed, workspaceSubscribePayload{WorkspaceIDs: authorised})
		})

		// workspace.unsubscribe — leave rooms.
		_ = s.On(eventWorkspaceUnsubscribe, func(args ...any) {
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

		// Registered once per socket. Cancels any in-flight dispatch goroutine so
		// agent-runtime requests are stopped and LLM tokens are not wasted for
		// clients that have already disconnected.
		_ = s.On("disconnect", func(args ...any) {
			reason := ""
			if len(args) > 0 {
				if r, ok := args[0].(string); ok {
					reason = r
				}
			}
			sd.Session.cancelCurrent()
			logger.Info("socketio: client disconnected",
				"socket_id", string(s.Id()),
				"subject", subject,
				"reason", reason,
			)
		})
	})
}

// RegisterMessageHandler adds the message.send event handler to the Socket.IO server.
// This is called separately from NewServer because the ChatService and AuthService
// are created after the Socket.IO server (which provides the Broadcaster that
// ChatService depends on — breaking the circular dependency).
//
// Registers a second "connection" listener on the /v1 namespace. Socket.IO
// supports multiple listeners, so this works alongside the existing connection
// handler that manages workspace subscriptions.
func RegisterMessageHandler(io *socket.Server, cfg *Config) {
	if cfg.DB == nil || cfg.ChatService == nil || cfg.AuthService == nil || cfg.WorkspaceService == nil {
		cfg.Logger.Info("socketio: message.send handler disabled (missing DB, ChatService, AuthService, or WorkspaceService)")
		return
	}

	shutdownCtx := cfg.ShutdownCtx
	if shutdownCtx == nil {
		shutdownCtx = context.Background()
	}

	h := &messageHandler{
		db:               cfg.DB,
		chatService:      cfg.ChatService,
		authService:      cfg.AuthService,
		workspaceService: cfg.WorkspaceService,
		logger:           cfg.Logger,
		shutdownCtx:      shutdownCtx,
	}

	nsp := io.Of(socketNamespace, nil)
	_ = nsp.On("connection", func(args ...any) {
		s, ok := args[0].(*socket.Socket)
		if !ok {
			return
		}
		_ = s.On(eventMessageSend, func(msgArgs ...any) {
			h.handleMessageSend(s, msgArgs...)
		})
	})

	cfg.Logger.Info("socketio: message.send handler registered")
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

// principalSubjectFromSocket extracts the principal subject stored in socket.Data().
// After the connection handler runs, Data() holds a *socketData; before it runs (e.g.
// during auth middleware) it holds a *orchestrator.Principal directly.
// Returns empty string if unavailable.
func principalSubjectFromSocket(s *socket.Socket) string {
	if s == nil {
		return ""
	}
	switch d := s.Data().(type) {
	case *socketData:
		if d == nil || d.Principal == nil {
			return ""
		}
		return d.Principal.Subject
	case *orchestrator.Principal:
		if d == nil {
			return ""
		}
		return d.Subject
	default:
		return ""
	}
}
