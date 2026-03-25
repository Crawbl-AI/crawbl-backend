package server

import (
	"log/slog"
	"net/http"
	"time"

	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/gocraft/dbr/v2"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
)

const (
	DefaultServerPort        = "7171"
	DefaultReadHeaderTimeout = 5 * time.Second
	DefaultDevTokenPrefix    = "dev"
)

type Config struct {
	Port string
}

type NewServerOpts struct {
	DB               *dbr.Connection
	Logger           *slog.Logger
	AuthService      orchestratorservice.AuthService
	WorkspaceService orchestratorservice.WorkspaceService
	ChatService      orchestratorservice.ChatService
	HTTPMiddleware   *httpserver.MiddlewareConfig
}

type Server struct {
	httpServer       *http.Server
	db               *dbr.Connection
	logger           *slog.Logger
	authService      orchestratorservice.AuthService
	workspaceService orchestratorservice.WorkspaceService
	chatService      orchestratorservice.ChatService
	httpMiddleware   *httpserver.MiddlewareConfig
}

type healthCheckResponse struct {
	Online  bool   `json:"online"`
	Version string `json:"version"`
}
