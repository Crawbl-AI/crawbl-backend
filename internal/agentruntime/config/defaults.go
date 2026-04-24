package config

import (
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/redisclient"
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
