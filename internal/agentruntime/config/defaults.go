package config

import (
	"fmt"
	"time"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/redisclient"
)

// Default values for fields the agent runtime controls. Database and
// Redis defaults live in internal/pkg/database and internal/pkg/redisclient
// respectively and are composed in via DefaultConfig() below — the
// agent runtime does not duplicate them.

// DefaultGRPCListen is the port the runtime serves gRPC on inside the
// workspace pod. Derived from crawblv1alpha1.DefaultGatewayPort so there
// is a single source of truth for the port number across the operator,
// the runtime, and any tooling that probes the pod.
var DefaultGRPCListen = fmt.Sprintf(":%d", crawblv1alpha1.DefaultGatewayPort)

const (
	// DefaultOpenAIModel is the OpenAI model identifier. Matches the
	// orchestrator's default so a workspace that does not override its
	// model setting resolves to the same model on both sides of the
	// pipe.
	DefaultOpenAIModel = "gpt-5-mini"

	// DefaultGracefulShutdownTimeout bounds the time the server waits
	// for in-flight streams to finish on SIGTERM before forcing close.
	DefaultGracefulShutdownTimeout = 30 * time.Second

	// DefaultBlueprintFetchTimeout is how long main.go will wait for
	// the orchestrator's GetWorkspaceBlueprint call before aborting
	// startup.
	DefaultBlueprintFetchTimeout = 15 * time.Second

	// DefaultRedisSessionTTL caps how long an idle ADK session lives
	// in Redis before Redis garbage collects it.
	DefaultRedisSessionTTL = 24 * time.Hour

	// DefaultSearXNGEndpoint points at the in-cluster SearXNG service
	// deployed by crawbl-argocd-apps/components/searxng/. Local dev
	// runs pointed at a different instance can override via
	// CRAWBL_SEARXNG_ENDPOINT.
	DefaultSearXNGEndpoint = "http://searxng.backend.svc.cluster.local:8080"
)

// DefaultConfig returns a Config populated with safe defaults for
// every field that has one. Required fields (WorkspaceID, UserID,
// MCPSigningKey, OpenAI.APIKey, orchestrator endpoints) are left
// empty and must be supplied by the caller or validated by Load().
func DefaultConfig() Config {
	return Config{
		GRPCListen: DefaultGRPCListen,
		OpenAI: OpenAIConfig{
			ModelName: DefaultOpenAIModel,
		},
		Redis: redisclient.Config{
			Addr: redisclient.DefaultAddr,
			DB:   redisclient.DefaultDB,
		},
		RedisSessionTTL: DefaultRedisSessionTTL,
		SearXNGEndpoint: DefaultSearXNGEndpoint,
		Startup: StartupConfig{
			GracefulShutdownTimeout: DefaultGracefulShutdownTimeout,
			BlueprintFetchTimeout:   DefaultBlueprintFetchTimeout,
		},
	}
}
