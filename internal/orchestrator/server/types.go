package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gocraft/dbr/v2"

	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
)

// Server configuration constants defining default values for the orchestrator HTTP server.
const (
	// DefaultServerPort is the default TCP port for the HTTP server if not specified.
	DefaultServerPort = "7171"

	// DefaultReadHeaderTimeout is the maximum duration for reading request headers.
	// This prevents slowloris attacks by timing out slow clients.
	DefaultReadHeaderTimeout = 5 * time.Second

	// DefaultReadTimeout is the maximum duration for reading the entire request,
	// including the body.
	DefaultReadTimeout = 1 * time.Minute

	// DefaultWriteTimeout is the maximum duration before timing out writes of the
	// response. Set generously to accommodate long-running agent streaming responses.
	DefaultWriteTimeout = 5 * time.Minute
)

// Config holds the configuration settings for the HTTP server.
// All fields are required for the server to function properly.
type Config struct {
	// Port is the TCP port on which the server listens for incoming connections.
	Port string

	// E2EToken is a shared secret that enables the dev-only /v1/e2e/query endpoint.
	// When empty, the endpoint is not registered. Must never be set in production.
	E2EToken string
}

// NewServerOpts contains all dependencies required to create a new Server instance.
// Each field is validated at server creation time to ensure proper initialization.
type NewServerOpts struct {
	// DB is the database connection pool for all persistence operations.
	DB *dbr.Connection

	// Logger provides structured logging throughout the server lifecycle.
	Logger *slog.Logger

	// AuthService handles user authentication, registration, and profile management.
	AuthService orchestratorservice.AuthService

	// WorkspaceService manages workspace provisioning and runtime state.
	WorkspaceService orchestratorservice.WorkspaceService

	// ChatService handles conversations, messages, and agent interactions.
	ChatService orchestratorservice.ChatService

	// AgentService handles agent details, settings, tools, and history retrieval.
	AgentService orchestratorservice.AgentService

	// HTTPMiddleware contains authentication and request middleware configuration.
	HTTPMiddleware *httpserver.MiddlewareConfig

	// Broadcaster emits real-time events to connected WebSocket clients.
	// If nil, a NopBroadcaster is used (no real-time events).
	Broadcaster realtime.Broadcaster

	// SocketIOHandler is the HTTP handler for the Socket.IO server.
	// If nil, Socket.IO is not mounted and the server is HTTP-only.
	SocketIOHandler http.Handler

	// RuntimeClient manages agent runtime CRs for workspace provisioning and cleanup.
	// Used by the delete handler to remove runtimes when a user is deleted.
	RuntimeClient userswarmclient.Client

	// MCPHandler is the HTTP handler for the MCP server.
	// If nil, the MCP endpoint is not mounted.
	MCPHandler http.Handler

	// IntegrationService manages third-party OAuth connections.
	// If nil, integration endpoints return service-unavailable errors.
	IntegrationService orchestratorservice.IntegrationService

	// MCPSigningKey is the HMAC signing key for internal MCP/runtime bearer tokens.
	MCPSigningKey string

	// RiverUIHandler is the HTTP handler for the River job dashboard (riverui).
	// When non-nil and RiverUIHost is set, requests whose Host header matches
	// RiverUIHost are routed to this handler instead of the API router.
	// Auth is enforced at the Envoy Gateway layer (SecurityPolicy basic auth)
	// so no application-level auth middleware is applied here.
	RiverUIHandler http.Handler

	// RiverUIHost is the hostname (without port) that activates the River UI.
	// Example: "dev.river.crawbl.com". When empty, RiverUIHandler is ignored
	// and the server serves only the API (feature flag off).
	RiverUIHost string
}

// Server is the orchestrator HTTP server that handles all mobile-facing API requests.
// It provides authentication, workspace management, chat functionality, and
// real-time WebSocket communication via Socket.IO while acting as the control
// plane between mobile clients and agent runtimes.
type Server struct {
	// httpServer is the underlying HTTP server instance.
	httpServer *http.Server

	// db is the database connection for all persistent storage operations.
	db *dbr.Connection

	// logger provides structured logging for server operations and request handling.
	logger *slog.Logger

	// authService handles authentication operations including sign-in, sign-up, and profile management.
	authService orchestratorservice.AuthService

	// workspaceService manages workspace lifecycle and runtime state queries.
	workspaceService orchestratorservice.WorkspaceService

	// chatService handles conversations, messages, and agent interactions.
	chatService orchestratorservice.ChatService

	// agentService handles agent details, settings, tools, and history retrieval.
	agentService orchestratorservice.AgentService

	// httpMiddleware contains authentication and request processing middleware.
	httpMiddleware *httpserver.MiddlewareConfig

	// broadcaster emits real-time events to connected WebSocket clients.
	broadcaster realtime.Broadcaster

	// runtimeClient manages agent runtime CRs. Used to delete runtimes on user deletion.
	runtimeClient userswarmclient.Client

	// integrationService manages third-party OAuth connections.
	integrationService orchestratorservice.IntegrationService

	// mcpSigningKey is the HMAC signing key for internal bearer tokens.
	mcpSigningKey string

	// cfg holds the server configuration.
	cfg *Config
}
