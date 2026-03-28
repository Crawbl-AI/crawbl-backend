// Package orchestrator provides the orchestrator HTTP server and migration subcommands.
package orchestrator

import (
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/spf13/cobra"

	orch "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/agentrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/conversationrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/messagerepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/userrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/workspacerepo"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server"
	authservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/authservice"
	chatservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/chatservice"
	workspaceservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/workspaceservice"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/redisclient"
	backendruntime "github.com/Crawbl-AI/crawbl-backend/internal/pkg/runtime"
)

const shutdownTimeout = 10 * time.Second

// NewOrchestratorCommand creates the "orchestrator" parent command.
// Running it directly starts the HTTP server; "migrate" is a subcommand.
func NewOrchestratorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "orchestrator",
		Short: "Run the orchestrator HTTP server",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runServer()
		},
	}

	cmd.AddCommand(newMigrateCommand())

	return cmd
}

func runServer() error {
	logLevel := slog.LevelInfo
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LOG_LEVEL"))) {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn", "warning":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))

	db, userRepo, workspaceRepo, agentRepo, conversationRepo, messageRepo, cleanup := mustBuildRepos(logger)
	defer cleanup()

	runtimeClient, err := buildRuntimeClient(logger)
	if err != nil {
		return err
	}
	httpMiddleware := buildHTTPMiddleware()

	broadcaster, socketIOHandler, cleanupRT := buildRealtime(logger, httpMiddleware)
	defer cleanupRT()

	workspaceService := workspaceservice.New(workspaceRepo, runtimeClient, logger)
	authService := authservice.New(userRepo, workspaceService, legalDocumentsFromEnv())
	chatService := chatservice.New(workspaceRepo, agentRepo, conversationRepo, messageRepo, runtimeClient, broadcaster)

	srv := server.NewServer(&server.Config{
		Port: envOrDefault("CRAWBL_SERVER_PORT", server.DefaultServerPort),
	}, &server.NewServerOpts{
		DB:               db,
		Logger:           logger,
		AuthService:      authService,
		WorkspaceService: workspaceService,
		ChatService:      chatService,
		HTTPMiddleware:   httpMiddleware,
		Broadcaster:      broadcaster,
		SocketIOHandler:  socketIOHandler,
		RuntimeClient:    runtimeClient,
	})

	return backendruntime.RunUntilSignal(srv.ListenAndServe, srv.Shutdown, shutdownTimeout)
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func buildHTTPMiddleware() *httpserver.MiddlewareConfig {
	return &httpserver.MiddlewareConfig{
		Environment: envOrDefault("CRAWBL_ENVIRONMENT", httpserver.EnvironmentLocal),
		E2EToken:    os.Getenv("CRAWBL_E2E_TOKEN"),
	}
}

func mustBuildRepos(logger *slog.Logger) (
	*dbr.Connection,
	orchestratorrepo.UserRepo,
	orchestratorrepo.WorkspaceRepo,
	orchestratorrepo.AgentRepo,
	orchestratorrepo.ConversationRepo,
	orchestratorrepo.MessageRepo,
	func(),
) {
	logger.Info("configuring storage backend", slog.String("backend", "postgres"))
	dbConfig := database.ConfigFromEnv("CRAWBL_")
	if err := database.EnsureSchema(dbConfig); err != nil {
		log.Fatal(err)
	}
	db, err := database.New(dbConfig)
	if err != nil {
		log.Fatal(err)
	}
	return db, userrepo.New(), workspacerepo.New(), agentrepo.New(), conversationrepo.New(), messagerepo.New(), func() {
		_ = db.Close()
	}
}

func buildRuntimeClient(logger *slog.Logger) (userswarmclient.Client, error) {
	cfg := userswarmclient.Config{
		Driver:          envOrDefault("CRAWBL_RUNTIME_DRIVER", userswarmclient.DriverFake),
		FakeReplyPrefix: envOrDefault("CRAWBL_RUNTIME_FAKE_REPLY_PREFIX", userswarmclient.DefaultFakeReplyPrefix),
		UserSwarm: userswarmclient.UserSwarmConfig{
			RuntimeNamespace:    envOrDefault("CRAWBL_RUNTIME_NAMESPACE", userswarmclient.DefaultRuntimeNamespace),
			Image:               strings.TrimSpace(os.Getenv("CRAWBL_RUNTIME_IMAGE")),
			ImagePullSecretName: strings.TrimSpace(os.Getenv("CRAWBL_RUNTIME_IMAGE_PULL_SECRET")),
			StorageSize:         envOrDefault("CRAWBL_RUNTIME_STORAGE_SIZE", userswarmclient.DefaultRuntimeStorageSize),
			StorageClassName:    strings.TrimSpace(os.Getenv("CRAWBL_RUNTIME_STORAGE_CLASS_NAME")),
			DefaultProvider:     envOrDefault("CRAWBL_RUNTIME_DEFAULT_PROVIDER", "openai"),
			DefaultModel:        envOrDefault("CRAWBL_RUNTIME_DEFAULT_MODEL", "gpt-5-mini"),
			EnvSecretName:       strings.TrimSpace(os.Getenv("CRAWBL_RUNTIME_ENV_SECRET_NAME")),
			TOMLOverrides:       strings.TrimSpace(os.Getenv("CRAWBL_RUNTIME_TOML_OVERRIDES")),
			PollTimeout:         durationFromEnv("CRAWBL_RUNTIME_POLL_TIMEOUT", userswarmclient.DefaultPollTimeout),
			PollInterval:        durationFromEnv("CRAWBL_RUNTIME_POLL_INTERVAL", userswarmclient.DefaultPollInterval),
			Port:                int32FromEnv("CRAWBL_RUNTIME_PORT", userswarmclient.DefaultRuntimePort),
		},
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Driver)) {
	case "", userswarmclient.DriverFake:
		logger.Info("configured fake runtime client")
		return userswarmclient.NewFakeClient(cfg), nil
	case userswarmclient.DriverUserSwarm:
		client, err := userswarmclient.NewUserSwarmClient(cfg)
		if err != nil {
			return nil, err
		}
		logger.Info("configured userswarm runtime client", slog.String("namespace", cfg.UserSwarm.RuntimeNamespace))
		return client, nil
	default:
		return nil, fmt.Errorf("unsupported runtime driver %q", cfg.Driver)
	}
}

func legalDocumentsFromEnv() *orch.LegalDocuments {
	return &orch.LegalDocuments{
		TermsOfService:        envOrDefault("CRAWBL_LEGAL_TERMS_OF_SERVICE", "https://crawbl.com/terms"),
		PrivacyPolicy:         envOrDefault("CRAWBL_LEGAL_PRIVACY_POLICY", "https://crawbl.com/privacy"),
		TermsOfServiceVersion: envOrDefault("CRAWBL_LEGAL_TERMS_OF_SERVICE_VERSION", "v1"),
		PrivacyPolicyVersion:  envOrDefault("CRAWBL_LEGAL_PRIVACY_POLICY_VERSION", "v1"),
	}
}

func durationFromEnv(key string, fallback time.Duration) time.Duration {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func int32FromEnv(key string, fallback int32) int32 {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil {
			return int32(n)
		}
	}
	return fallback
}

func buildRealtime(logger *slog.Logger, middleware *httpserver.MiddlewareConfig) (realtime.Broadcaster, http.Handler, func()) {
	addr := strings.TrimSpace(os.Getenv("CRAWBL_REDIS_ADDR"))
	if addr == "" {
		logger.Info("realtime disabled: CRAWBL_REDIS_ADDR not set")
		return realtime.NopBroadcaster{}, nil, func() {}
	}

	redisCfg := redisclient.ConfigFromEnv("CRAWBL_")
	rc, err := redisclient.New(redisCfg)
	if err != nil {
		logger.Error("failed to connect to Redis, falling back to no realtime", "error", err)
		return realtime.NopBroadcaster{}, nil, func() {}
	}
	logger.Info("redis connected", slog.String("addr", redisCfg.Addr))

	io := server.NewSocketIOServer(&server.SocketIOConfig{
		Logger:      logger,
		RedisClient: redisclient.Unwrap(rc),
	})

	broadcaster := server.NewSocketIOBroadcaster(io, logger)
	handler := server.SocketIOHandler(io)

	cleanup := func() {
		io.Close(nil)
		_ = rc.Close()
	}

	logger.Info("realtime enabled: socket.io + redis")
	return broadcaster, handler, cleanup
}
