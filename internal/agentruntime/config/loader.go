package config

import (
	"flag"
	"fmt"
	"io"
	"os"
)

// Load parses configuration from CLI flags first, then fills unset fields
// from environment variables, then fills remaining gaps from DefaultConfig.
//
// CLI flag precedence:
//
//	--grpc-listen      > CRAWBL_GRPC_LISTEN      > DefaultGRPCListen
//	--workspace-id     > CRAWBL_WORKSPACE_ID     (required)
//	--user-id          > CRAWBL_USER_ID          (required)
//	--orchestrator-endpoint > CRAWBL_ORCHESTRATOR_ENDPOINT (required)
//	--mcp-endpoint     > CRAWBL_MCP_ENDPOINT     (required)
//	                     CRAWBL_MCP_SIGNING_KEY (required, env only)
//	--openai-model     > CRAWBL_OPENAI_MODEL     > DefaultOpenAIModel
//	--openai-base-url  > CRAWBL_OPENAI_BASE_URL  (optional)
//	                     OPENAI_API_KEY          (required, env only)
//	                     CRAWBL_SPACES_*         (Phase 2+, all optional Phase 1)
//
// Required fields that are still empty after loading produce a validation
// error with every missing field named. Secrets are never echoed in the
// error message.
func Load(args []string, stderr io.Writer) (Config, error) {
	cfg := DefaultConfig()

	fs := flag.NewFlagSet("crawbl-agent-runtime", flag.ContinueOnError)
	fs.SetOutput(stderr)

	fs.StringVar(&cfg.GRPCListen, "grpc-listen", envOr("CRAWBL_GRPC_LISTEN", cfg.GRPCListen), "gRPC listen address (host:port)")
	fs.StringVar(&cfg.WorkspaceID, "workspace-id", os.Getenv("CRAWBL_WORKSPACE_ID"), "Crawbl workspace ID this runtime instance serves")
	fs.StringVar(&cfg.UserID, "user-id", os.Getenv("CRAWBL_USER_ID"), "Crawbl user ID owning the workspace")
	fs.StringVar(&cfg.OrchestratorGRPCEndpoint, "orchestrator-endpoint", os.Getenv("CRAWBL_ORCHESTRATOR_ENDPOINT"), "Orchestrator internal gRPC endpoint (host:port)")
	fs.StringVar(&cfg.MCPEndpoint, "mcp-endpoint", os.Getenv("CRAWBL_MCP_ENDPOINT"), "Orchestrator MCP HTTP endpoint URL")
	fs.StringVar(&cfg.OpenAI.ModelName, "openai-model", envOr("CRAWBL_OPENAI_MODEL", cfg.OpenAI.ModelName), "OpenAI model name")
	fs.StringVar(&cfg.OpenAI.BaseURL, "openai-base-url", os.Getenv("CRAWBL_OPENAI_BASE_URL"), "OpenAI-compatible endpoint override (Ollama, Azure, OpenRouter)")
	fs.StringVar(&cfg.Spaces.Endpoint, "spaces-endpoint", os.Getenv("CRAWBL_SPACES_ENDPOINT"), "DigitalOcean Spaces endpoint (e.g. https://fra1.digitaloceanspaces.com)")
	fs.StringVar(&cfg.Spaces.Region, "spaces-region", os.Getenv("CRAWBL_SPACES_REGION"), "DigitalOcean Spaces region")
	fs.StringVar(&cfg.Spaces.Bucket, "spaces-bucket", os.Getenv("CRAWBL_SPACES_BUCKET"), "DigitalOcean Spaces bucket name")

	if err := fs.Parse(args); err != nil {
		return Config{}, fmt.Errorf("parse flags: %w", err)
	}

	// Env-only secrets never have flag equivalents.
	cfg.MCPSigningKey = os.Getenv("CRAWBL_MCP_SIGNING_KEY")
	cfg.OpenAI.APIKey = os.Getenv("OPENAI_API_KEY")
	cfg.Spaces.AccessKey = os.Getenv("CRAWBL_SPACES_ACCESS_KEY")
	cfg.Spaces.SecretKey = os.Getenv("CRAWBL_SPACES_SECRET_KEY")

	return cfg, nil
}

// Validate enforces the required-field rules. Separate from Load() so unit
// tests can construct Configs directly and call Validate in isolation.
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
