package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
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
	backendruntime "github.com/Crawbl-AI/crawbl-backend/internal/pkg/runtime"
)

const (
	shutdownTimeout = 10 * time.Second
)

func newServerCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "server",
		Short: "Run the orchestrator HTTP server",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runServer()
		},
	}
}

func runServer() error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	db, userRepo, workspaceRepo, agentRepo, conversationRepo, messageRepo, cleanup := mustBuildRepos(logger)
	defer cleanup()

	runtimeClient, err := buildRuntimeClient(logger)
	if err != nil {
		return err
	}
	workspaceService := workspaceservice.New(workspaceRepo, runtimeClient, logger)
	authService := authservice.New(userRepo, workspaceService, legalDocumentsFromEnv())
	chatService := chatservice.New(workspaceRepo, agentRepo, conversationRepo, messageRepo, runtimeClient)
	httpMiddleware, err := buildHTTPMiddleware(logger)
	if err != nil {
		return err
	}

	srv := server.NewServer(&server.Config{
		Port: envOrDefault("CRAWBL_SERVER_PORT", server.DefaultServerPort),
	}, &server.NewServerOpts{
		DB:               db,
		Logger:           logger,
		AuthService:      authService,
		WorkspaceService: workspaceService,
		ChatService:      chatService,
		HTTPMiddleware:   httpMiddleware,
	})

	return backendruntime.RunUntilSignal(srv.ListenAndServe, srv.Shutdown, shutdownTimeout)
}

func envOrDefault(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

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

func buildIdentityVerifier(ctx context.Context, logger *slog.Logger, environment string) (orchestrator.IdentityVerifier, error) {
	verifiers := make([]orchestrator.IdentityVerifier, 0, 2)
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

func isNonProductionEnvironment(environment string) bool {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case httpserver.EnvironmentLocal, "test":
		return true
	default:
		return false
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
		_ = db.DB.Close()
	}
}

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

func legalDocumentsFromEnv() *orchestrator.LegalDocuments {
	return &orchestrator.LegalDocuments{
		TermsOfService:        envOrDefault("CRAWBL_LEGAL_TERMS_OF_SERVICE", "https://crawbl.com/terms"),
		PrivacyPolicy:         envOrDefault("CRAWBL_LEGAL_PRIVACY_POLICY", "https://crawbl.com/privacy"),
		TermsOfServiceVersion: envOrDefault("CRAWBL_LEGAL_TERMS_OF_SERVICE_VERSION", "v1"),
		PrivacyPolicyVersion:  envOrDefault("CRAWBL_LEGAL_PRIVACY_POLICY_VERSION", "v1"),
	}
}

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
