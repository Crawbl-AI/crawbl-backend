package main

import (
	"context"
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

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/agentrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/conversationrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/messagerepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/userrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/workspacerepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/runtimeclient"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/server"
	authservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/authservice"
	chatservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/chatservice"
	workspaceservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service/workspaceservice"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/configenv"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	backendfirebase "github.com/Crawbl-AI/crawbl-backend/internal/pkg/firebase"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/httpserver"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/redisclient"
	backendruntime "github.com/Crawbl-AI/crawbl-backend/internal/pkg/runtime"
)

// shutdownTimeout defines the maximum duration to wait for graceful
// server shutdown when a termination signal is received.
const shutdownTimeout = 10 * time.Second

// defaultVerifierCapacity is the initial capacity for the verifiers slice.
const defaultVerifierCapacity = 2

// newServerCommand creates the "server" subcommand for starting the HTTP API.
// This command initializes and runs the orchestrator HTTP server with all
// configured services, repositories, and middleware.
func newServerCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "server",
		Short: "Run the orchestrator HTTP server",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runServer()
		},
	}
}

// runServer initializes and starts the orchestrator HTTP server.
// It performs the following steps:
//   - Configures structured logging based on LOG_LEVEL environment variable
//   - Builds database connections and repository instances
//   - Creates the runtime client for UserSwarm communication
//   - Initializes authentication, workspace, and chat services
//   - Configures HTTP middleware including identity verification
//   - Starts the server with graceful shutdown handling
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
	httpMiddleware, err := buildHTTPMiddleware(logger)
	if err != nil {
		return err
	}

	// Build the real-time layer: Redis → Socket.IO → Broadcaster.
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
	})

	return backendruntime.RunUntilSignal(srv.ListenAndServe, srv.Shutdown, shutdownTimeout)
}

// envOrDefault retrieves an environment variable value or returns a fallback
// if the variable is not set or empty.
func envOrDefault(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

// boolFromEnv parses a boolean value from an environment variable.
// Accepts common boolean representations: "1", "true", "yes", "y", "on" for true
// and "0", "false", "no", "n", "off" for false.
// Returns the fallback value if the variable is unset or has an unrecognized format.
func boolFromEnv(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	switch strings.ToLower(value) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

// buildHTTPMiddleware constructs the HTTP middleware configuration including
// environment settings and identity verification.
// It creates identity verifiers based on the environment (Firebase for production,
// dev tokens for local/test environments).
func buildHTTPMiddleware(logger *slog.Logger) (*httpserver.MiddlewareConfig, error) {
	environment := envOrDefault("CRAWBL_ENVIRONMENT", httpserver.EnvironmentLocal)
	verifier, err := buildIdentityVerifier(context.Background(), logger, environment)
	if err != nil {
		return nil, err
	}

	return &httpserver.MiddlewareConfig{
		Environment:      environment,
		HMACSecret:       configenv.SecretString("CRAWBL_HTTP_HMAC_SECRET", ""),
		IdentityVerifier: verifier,
	}, nil
}

// buildIdentityVerifier creates the appropriate identity verifier(s) based on
// the environment and configuration.
// In non-production environments or when CRAWBL_AUTH_ALLOW_DEV_TOKENS is true,
// a development token verifier is added.
// If Firebase credentials are configured, a Firebase token verifier is added.
// Returns an error if no verifier can be configured.
func buildIdentityVerifier(ctx context.Context, logger *slog.Logger, environment string) (orchestrator.IdentityVerifier, error) {
	verifiers := make([]orchestrator.IdentityVerifier, 0, defaultVerifierCapacity)
	if isNonProductionEnvironment(environment) || boolFromEnv("CRAWBL_AUTH_ALLOW_DEV_TOKENS", false) {
		verifiers = append(verifiers, orchestrator.NewDevTokenVerifier(envOrDefault("AUTH_DEV_TOKEN_PREFIX", server.DefaultDevTokenPrefix)))
	}

	firebaseConfig := backendfirebase.Config{
		CredentialsFile: strings.TrimSpace(os.Getenv("CRAWBL_FIREBASE_CREDENTIALS_FILE")),
		CredentialsJSON: strings.TrimSpace(os.Getenv("CRAWBL_FIREBASE_CREDENTIALS_JSON")),
	}
	if firebaseConfig.Enabled() {
		app, err := backendfirebase.New(ctx, firebaseConfig)
		if err != nil {
			return nil, err
		}
		verifiers = append(verifiers, orchestrator.NewFirebaseTokenVerifier(app, environment))
		logger.Info("configured firebase identity verifier")
	}

	if len(verifiers) == 0 {
		return nil, fmt.Errorf("no identity verifier configured; set Firebase credentials or enable local dev tokens")
	}

	return orchestrator.NewChainIdentityVerifier(verifiers...), nil
}

// isNonProductionEnvironment returns true if the environment is a local
// development or test environment where development token authentication
// should be permitted.
func isNonProductionEnvironment(environment string) bool {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case httpserver.EnvironmentLocal, "test":
		return true
	default:
		return false
	}
}

// mustBuildRepos initializes the database connection and all repository instances
// required by the orchestrator server. It ensures the database schema exists
// and returns repositories for users, workspaces, agents, conversations, and messages.
// The returned cleanup function should be called to close the database connection.
// This function logs fatal errors and exits if initialization fails.
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

// buildRuntimeClient creates the runtime client for communicating with UserSwarm
// instances. The client type is determined by the CRAWBL_RUNTIME_DRIVER environment
// variable:
//   - "fake" or empty: Creates a fake client for local development/testing
//   - "userswarm": Creates a Kubernetes-based client for production cluster deployment
//
// Returns an error if an unsupported driver is specified.
func buildRuntimeClient(logger *slog.Logger) (runtimeclient.Client, error) {
	cfg := runtimeclient.Config{
		Driver:          envOrDefault("CRAWBL_RUNTIME_DRIVER", runtimeclient.DriverFake),
		FakeReplyPrefix: envOrDefault("CRAWBL_RUNTIME_FAKE_REPLY_PREFIX", runtimeclient.DefaultFakeReplyPrefix),
		UserSwarm: runtimeclient.UserSwarmConfig{
			RuntimeNamespace:    envOrDefault("CRAWBL_RUNTIME_NAMESPACE", runtimeclient.DefaultRuntimeNamespace),
			Image:               strings.TrimSpace(os.Getenv("CRAWBL_RUNTIME_IMAGE")),
			ImagePullSecretName: strings.TrimSpace(os.Getenv("CRAWBL_RUNTIME_IMAGE_PULL_SECRET")),
			StorageSize:         envOrDefault("CRAWBL_RUNTIME_STORAGE_SIZE", runtimeclient.DefaultRuntimeStorageSize),
			StorageClassName:    strings.TrimSpace(os.Getenv("CRAWBL_RUNTIME_STORAGE_CLASS_NAME")),
			DefaultProvider:     envOrDefault("CRAWBL_RUNTIME_DEFAULT_PROVIDER", "openai"),
			DefaultModel:        envOrDefault("CRAWBL_RUNTIME_DEFAULT_MODEL", "gpt-5.4"),
			EnvSecretName:       strings.TrimSpace(os.Getenv("CRAWBL_RUNTIME_ENV_SECRET_NAME")),
			TOMLOverrides:       strings.TrimSpace(os.Getenv("CRAWBL_RUNTIME_TOML_OVERRIDES")),
			PollTimeout:         durationFromEnv("CRAWBL_RUNTIME_POLL_TIMEOUT", runtimeclient.DefaultPollTimeout),
			PollInterval:        durationFromEnv("CRAWBL_RUNTIME_POLL_INTERVAL", runtimeclient.DefaultPollInterval),
			Port:                int32FromEnv("CRAWBL_RUNTIME_PORT", runtimeclient.DefaultRuntimePort),
		},
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Driver)) {
	case "", runtimeclient.DriverFake:
		logger.Info("configured fake runtime client")
		return runtimeclient.NewFakeClient(cfg), nil
	case runtimeclient.DriverUserSwarm:
		client, err := runtimeclient.NewUserSwarmClient(cfg)
		if err != nil {
			return nil, err
		}
		logger.Info("configured userswarm runtime client", slog.String("namespace", cfg.UserSwarm.RuntimeNamespace))
		return client, nil
	default:
		return nil, fmt.Errorf("unsupported runtime driver %q", cfg.Driver)
	}
}

// legalDocumentsFromEnv constructs the legal documents configuration from
// environment variables. Returns default Crawbl URLs if not explicitly configured.
func legalDocumentsFromEnv() *orchestrator.LegalDocuments {
	return &orchestrator.LegalDocuments{
		TermsOfService:        envOrDefault("CRAWBL_LEGAL_TERMS_OF_SERVICE", "https://crawbl.com/terms"),
		PrivacyPolicy:         envOrDefault("CRAWBL_LEGAL_PRIVACY_POLICY", "https://crawbl.com/privacy"),
		TermsOfServiceVersion: envOrDefault("CRAWBL_LEGAL_TERMS_OF_SERVICE_VERSION", "v1"),
		PrivacyPolicyVersion:  envOrDefault("CRAWBL_LEGAL_PRIVACY_POLICY_VERSION", "v1"),
	}
}

// durationFromEnv parses a duration value from an environment variable.
// Accepts Go duration format (e.g., "30s", "5m", "1h").
// Returns the fallback value if the variable is unset or has an invalid format.
func durationFromEnv(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

// int32FromEnv parses an int32 value from an environment variable.
// Returns the fallback value if the variable is unset or has an invalid format.
func int32FromEnv(key string, fallback int32) int32 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return fallback
	}
	return int32(parsed)
}

// buildRealtime creates the Redis client, Socket.IO server with Redis adapter,
// and the broadcaster that emits real-time events to connected mobile clients.
// Returns a NopBroadcaster and nil handler if Redis is not configured (CRAWBL_REDIS_ADDR empty).
// The returned cleanup function closes the Redis connection.
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
		Logger:           logger,
		IdentityVerifier: middleware.IdentityVerifier,
		RedisClient:      redisclient.Unwrap(rc),
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
