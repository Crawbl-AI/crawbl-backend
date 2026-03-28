package zeroclaw

// This file contains all shared types, constants, and variables for the zeroclaw package.
//
// Declarations are grouped by origin:
//   - Operator config types (originally in config.go)
//   - TOML config types (originally in toml.go)
//   - Bootstrap constants and vars (originally in bootstrap.go)

// ---------------------------------------------------------------------------
// Operator config types
// ---------------------------------------------------------------------------

// ZeroClawConfig holds cluster-wide defaults loaded from the operator's config file.
// These values are baked into the per-user bootstrap config.toml at provisioning time.
// The file is typically mounted as a ConfigMap in the webhook pod.
type ZeroClawConfig struct {
	Defaults    ZeroClawDefaults    `yaml:"defaults"`
	HTTPRequest ZeroClawHTTPRequest `yaml:"httpRequest"`
	WebFetch    ZeroClawWebFetch    `yaml:"webFetch"`
	WebSearch   ZeroClawWebSearch   `yaml:"webSearch"`
	Autonomy    ZeroClawAutonomy    `yaml:"autonomy"`
}

// ZeroClawDefaults controls global provider behavior: temperature, timeouts, retries.
type ZeroClawDefaults struct {
	Temperature       float64 `yaml:"temperature"`
	Timeout           int     `yaml:"timeout"`
	ShortTimeout      int     `yaml:"shortTimeout"`
	ProviderRetries   uint32  `yaml:"providerRetries"`
	ProviderBackoffMs uint64  `yaml:"providerBackoffMs"`
}

// ZeroClawHTTPRequest controls the http_request tool (raw HTTP calls from the agent).
type ZeroClawHTTPRequest struct {
	MaxResponseSize int      `yaml:"maxResponseSize"`
	AllowedDomains  []string `yaml:"allowedDomains"`
}

// ZeroClawWebFetch controls the web_fetch tool (read web page content).
type ZeroClawWebFetch struct {
	MaxResponseSize int `yaml:"maxResponseSize"`
}

// ZeroClawWebSearch controls the web_search tool (internet search).
type ZeroClawWebSearch struct {
	Provider   string `yaml:"provider"`
	MaxResults int    `yaml:"maxResults"`
}

// ZeroClawAutonomy controls what the agent can do without user approval.
type ZeroClawAutonomy struct {
	AllowedCommands []string `yaml:"allowedCommands"`
	ForbiddenPaths  []string `yaml:"forbiddenPaths"`
	AutoApprove     []string `yaml:"autoApprove"`
}

// ---------------------------------------------------------------------------
// Defaults
// ---------------------------------------------------------------------------

// Built-in defaults used when no config file is provided or a field is missing.
const (
	DefaultTemperature          = 0.7
	DefaultMaxResponseSize      = 1_000_000
	DefaultTimeoutSecs          = 30
	DefaultMaxResponseSizeSmall = 500_000
	DefaultMaxResults           = 5
	DefaultTimeoutSecsShort     = 15
	DefaultProviderRetries      = 2
	DefaultProviderBackoffMs    = 500
)

// ---------------------------------------------------------------------------
// TOML config types
// ---------------------------------------------------------------------------

// BootstrapConfig is the top-level TOML structure that ZeroClaw reads from config.toml.
// Field names use snake_case to match ZeroClaw's expected TOML keys.
type BootstrapConfig struct {
	DefaultProvider    string            `toml:"default_provider"`
	DefaultModel       string            `toml:"default_model"`
	DefaultTemperature float64           `toml:"default_temperature"`
	Autonomy           AutonomyConfig    `toml:"autonomy"`
	HTTPRequest        HTTPRequestConfig `toml:"http_request"`
	WebFetch           WebFetchConfig    `toml:"web_fetch"`
	WebSearch          WebSearchConfig   `toml:"web_search"`
	Gateway            GatewayConfig     `toml:"gateway"`
	Reliability        Reliability       `toml:"reliability"`
}

// AutonomyConfig controls what the agent can do without asking the user.
type AutonomyConfig struct {
	Level           string   `toml:"level"`
	WorkspaceOnly   bool     `toml:"workspace_only"`
	AllowedCommands []string `toml:"allowed_commands"`
	ForbiddenPaths  []string `toml:"forbidden_paths"`
	AutoApprove     []string `toml:"auto_approve"`
	AlwaysAsk       []string `toml:"always_ask"`
}

// GatewayConfig controls ZeroClaw's HTTP gateway that the orchestrator talks to.
type GatewayConfig struct {
	Port            int32  `toml:"port"`
	Host            string `toml:"host"`
	AllowPublicBind bool   `toml:"allow_public_bind"`
	RequirePairing  bool   `toml:"require_pairing"`
}

// HTTPRequestConfig controls the agent's raw HTTP request tool.
type HTTPRequestConfig struct {
	Enabled           bool     `toml:"enabled"`
	AllowedDomains    []string `toml:"allowed_domains"`
	MaxResponseSize   int      `toml:"max_response_size"`
	TimeoutSecs       int      `toml:"timeout_secs"`
	AllowPrivateHosts bool     `toml:"allow_private_hosts"`
}

// WebFetchConfig controls the agent's web page fetching tool.
type WebFetchConfig struct {
	Enabled         bool     `toml:"enabled"`
	AllowedDomains  []string `toml:"allowed_domains"`
	BlockedDomains  []string `toml:"blocked_domains"`
	MaxResponseSize int      `toml:"max_response_size"`
	TimeoutSecs     int      `toml:"timeout_secs"`
}

// WebSearchConfig controls the agent's internet search tool.
type WebSearchConfig struct {
	Enabled            bool   `toml:"enabled"`
	Provider           string `toml:"provider"`
	MaxResults         int    `toml:"max_results"`
	TimeoutSecs        int    `toml:"timeout_secs"`
	SearxngInstanceURL string `toml:"searxng_instance_url,omitempty"`
}

// Reliability controls provider retry behavior and model fallbacks.
type Reliability struct {
	ProviderRetries uint32              `toml:"provider_retries"`
	ProviderBackoff uint64              `toml:"provider_backoff_ms"`
	ModelFallbacks  map[string][]string `toml:"model_fallbacks"`
}

// ---------------------------------------------------------------------------
// Bootstrap constants and vars
// ---------------------------------------------------------------------------

// configFilePerm is the permission for the live config file (owner read/write only).
const configFilePerm = 0o600

// managedKeys are the TOML keys the operator controls. Any key NOT listed here
// belongs to ZeroClaw and is never overwritten by the merge.
var managedKeys = map[string][]string{
	"":             {"default_provider", "default_model", "default_temperature"},
	"autonomy":     {"level", "workspace_only", "allowed_commands", "forbidden_paths", "auto_approve", "always_ask"},
	"http_request": {"enabled", "allowed_domains", "max_response_size", "timeout_secs", "allow_private_hosts"},
	"web_fetch":    {"enabled", "allowed_domains", "blocked_domains", "max_response_size", "timeout_secs"},
	"web_search":   {"enabled", "provider", "brave_api_key", "searxng_instance_url", "max_results", "timeout_secs"},
	"gateway":      {"port", "host", "allow_public_bind", "require_pairing"},
}

// apiKeyEnvVars is the priority-ordered list of environment variables checked
// for the LLM provider API key. The first non-empty value wins.
var apiKeyEnvVars = []string{
	"OPENAI_API_KEY",
	"USERSWARM_API_KEY",
	"ZEROCLAW_API_KEY",
	"API_KEY",
	"OPENROUTER_API_KEY",
	"ANTHROPIC_API_KEY",
	"GEMINI_API_KEY",
}
