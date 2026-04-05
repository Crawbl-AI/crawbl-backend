package config

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/redisclient"
)

// envPrefix is the common prefix used for every environment variable
// this binary reads. Matches the orchestrator's CRAWBL_* convention so
// a single .env file can drive both processes in docker-compose.
const envPrefix = "CRAWBL_"

// Load parses configuration from CLI flags first, then fills unset fields
// from environment variables, then fills remaining gaps from DefaultConfig.
//
// Flag / env precedence (highest wins):
//
//	--grpc-listen           > CRAWBL_GRPC_LISTEN           > DefaultGRPCListen
//	--workspace-id          > CRAWBL_WORKSPACE_ID          (required)
//	--user-id               > CRAWBL_USER_ID               (required)
//	--orchestrator-endpoint > CRAWBL_ORCHESTRATOR_ENDPOINT (required)
//	--mcp-endpoint          > CRAWBL_MCP_ENDPOINT          (required)
//	                          CRAWBL_MCP_SIGNING_KEY       (required, env only)
//	--openai-model          > CRAWBL_OPENAI_MODEL          > DefaultOpenAIModel
//	--openai-base-url       > CRAWBL_OPENAI_BASE_URL       (optional)
//	                          OPENAI_API_KEY               (required, env only)
//	                          CRAWBL_DATABASE_*            (required, via internal/pkg/database.ConfigFromEnv)
//	                          CRAWBL_REDIS_*               (required, via internal/pkg/redisclient.ConfigFromEnv)
//	--redis-session-ttl     > CRAWBL_REDIS_SESSION_TTL     > DefaultRedisSessionTTL
//
// Required fields that are still empty after loading produce a
// validation error naming every missing field. Secrets are never
// echoed in error messages.
func Load(args []string, stderr io.Writer) (Config, error) {
	cfg := DefaultConfig()

	// Let the shared packages populate Postgres and Redis from env
	// vars first so CLI flags can override specific fields afterwards.
	cfg.Postgres = database.ConfigFromEnv(envPrefix)
	cfg.Redis = redisclient.ConfigFromEnv(envPrefix)

	fs := flag.NewFlagSet("crawbl-agent-runtime", flag.ContinueOnError)
	fs.SetOutput(stderr)

	fs.StringVar(&cfg.GRPCListen, "grpc-listen", envOr("CRAWBL_GRPC_LISTEN", cfg.GRPCListen), "gRPC listen address (host:port)")
	fs.StringVar(&cfg.WorkspaceID, "workspace-id", os.Getenv("CRAWBL_WORKSPACE_ID"), "Crawbl workspace ID this runtime instance serves")
	fs.StringVar(&cfg.UserID, "user-id", os.Getenv("CRAWBL_USER_ID"), "Crawbl user ID owning the workspace")
	fs.StringVar(&cfg.OrchestratorGRPCEndpoint, "orchestrator-endpoint", os.Getenv("CRAWBL_ORCHESTRATOR_ENDPOINT"), "Orchestrator internal gRPC endpoint (host:port)")
	fs.StringVar(&cfg.MCPEndpoint, "mcp-endpoint", os.Getenv("CRAWBL_MCP_ENDPOINT"), "Orchestrator MCP HTTP endpoint URL")
	fs.StringVar(&cfg.OpenAI.ModelName, "openai-model", envOr("CRAWBL_OPENAI_MODEL", cfg.OpenAI.ModelName), "OpenAI model name")
	fs.StringVar(&cfg.OpenAI.BaseURL, "openai-base-url", os.Getenv("CRAWBL_OPENAI_BASE_URL"), "OpenAI-compatible endpoint override (Ollama, Azure, OpenRouter)")

	// Postgres overrides — CLI takes precedence over env when set.
	fs.StringVar(&cfg.Postgres.Host, "postgres-host", cfg.Postgres.Host, "Postgres host")
	fs.StringVar(&cfg.Postgres.Port, "postgres-port", cfg.Postgres.Port, "Postgres port")
	fs.StringVar(&cfg.Postgres.Name, "postgres-name", cfg.Postgres.Name, "Postgres database name")
	fs.StringVar(&cfg.Postgres.Schema, "postgres-schema", cfg.Postgres.Schema, "Postgres schema for agent_memories")

	// Redis overrides.
	fs.StringVar(&cfg.Redis.Addr, "redis-addr", cfg.Redis.Addr, "Redis address host:port")
	redisTTLFlag := fs.String("redis-session-ttl", "", "Redis session TTL (Go duration, e.g. 24h)")

	fs.StringVar(&cfg.SearXNGEndpoint, "searxng-endpoint", envOr("CRAWBL_SEARXNG_ENDPOINT", cfg.SearXNGEndpoint), "SearXNG meta-search base URL used by the web_search_tool")

	if err := fs.Parse(args); err != nil {
		return Config{}, fmt.Errorf("parse flags: %w", err)
	}

	// Env-only secrets never have flag equivalents.
	cfg.MCPSigningKey = os.Getenv("CRAWBL_MCP_SIGNING_KEY")
	cfg.OpenAI.APIKey = os.Getenv("OPENAI_API_KEY")

	// Resolve Redis session TTL: CLI flag > env > default.
	if strings.TrimSpace(*redisTTLFlag) != "" {
		d, err := time.ParseDuration(*redisTTLFlag)
		if err != nil {
			return Config{}, fmt.Errorf("parse --redis-session-ttl: %w", err)
		}
		cfg.RedisSessionTTL = d
	} else if v := os.Getenv("CRAWBL_REDIS_SESSION_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("parse CRAWBL_REDIS_SESSION_TTL: %w", err)
		}
		cfg.RedisSessionTTL = d
	}

	return cfg, nil
}

// Validate enforces the required-field rules. Separate from Load() so
// integration checks can construct Configs directly and call Validate
// in isolation.
func (c Config) Validate() error {
	var missing []string
	if c.WorkspaceID == "" {
		missing = append(missing, "workspace-id / CRAWBL_WORKSPACE_ID")
	}
	if c.UserID == "" {
		missing = append(missing, "user-id / CRAWBL_USER_ID")
	}
	if c.OrchestratorGRPCEndpoint == "" {
		missing = append(missing, "orchestrator-endpoint / CRAWBL_ORCHESTRATOR_ENDPOINT")
	}
	if c.MCPEndpoint == "" {
		missing = append(missing, "mcp-endpoint / CRAWBL_MCP_ENDPOINT")
	}
	if c.MCPSigningKey == "" {
		missing = append(missing, "CRAWBL_MCP_SIGNING_KEY")
	}
	if c.OpenAI.APIKey == "" {
		missing = append(missing, "OPENAI_API_KEY")
	}
	if c.Postgres.Host == "" {
		missing = append(missing, "CRAWBL_DATABASE_HOST")
	}
	if c.Postgres.Name == "" {
		missing = append(missing, "CRAWBL_DATABASE_NAME")
	}
	if c.Postgres.Password == "" {
		missing = append(missing, "CRAWBL_DATABASE_PASSWORD")
	}
	if c.Redis.Addr == "" {
		missing = append(missing, "CRAWBL_REDIS_ADDR")
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("missing required configuration: %v", missing)
}

// envOr returns the env var value when set, otherwise the fallback.
func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
