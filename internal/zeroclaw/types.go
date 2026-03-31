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
	Agents      map[string]ZeroClawAgent `yaml:"agents,omitempty"`
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

// ZeroClawAgent defines a delegate agent in the operator config.
// Maps to [agents.<name>] TOML sections in ZeroClaw's config.toml.
type ZeroClawAgent struct {
	SystemPrompt string   `yaml:"systemPrompt"`
	Agentic      bool     `yaml:"agentic"`
	AllowedTools []string `yaml:"allowedTools,omitempty"`
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
	MCP                MCPBootstrapConfig `toml:"mcp"`
	Agents             map[string]DelegateAgentConfig `toml:"agents,omitempty"`
}

// MCPBootstrapConfig controls ZeroClaw's MCP client connections.
// The orchestrator MCP server is injected here at provisioning time.
type MCPBootstrapConfig struct {
	Enabled         bool                    `toml:"enabled"`
	DeferredLoading bool                    `toml:"deferred_loading"`
	Servers         []MCPServerBootstrapConfig `toml:"servers"`
}

// MCPServerBootstrapConfig defines a single MCP server connection for ZeroClaw.
type MCPServerBootstrapConfig struct {
	Name            string            `toml:"name"`
	Transport       string            `toml:"transport"`
	URL             string            `toml:"url"`
	Headers         map[string]string `toml:"headers,omitempty"`
	ToolTimeoutSecs int               `toml:"tool_timeout_secs,omitempty"`
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

// DelegateAgentConfig defines a sub-agent in ZeroClaw's native delegate system.
// Maps to ZeroClaw's Rust DelegateAgentConfig in src/config/schema.rs.
type DelegateAgentConfig struct {
	Provider     string   `toml:"provider"`
	Model        string   `toml:"model,omitempty"`
	SystemPrompt string   `toml:"system_prompt,omitempty"`
	Agentic      bool     `toml:"agentic"`
	AllowedTools []string `toml:"allowed_tools,omitempty"`
	SkillsDir    string   `toml:"skills_directory,omitempty"`
}

// ---------------------------------------------------------------------------
// Bootstrap constants and vars
// ---------------------------------------------------------------------------

// configFilePerm is the permission for the live config file (owner read/write only).
const configFilePerm = 0o600

// mergeStrategy defines how a TOML section is merged during bootstrap.
type mergeStrategy int

const (
	// mergeKeys copies only the listed keys from bootstrap into live.
	// Keys not in the list are preserved in live (ZeroClaw-owned).
	mergeKeys mergeStrategy = iota

	// replaceSection replaces the entire section from bootstrap.
	// Used when the operator defines the complete set of entries.
	replaceSection
)

// managedSection describes an operator-controlled TOML section and how to merge it.
type managedSection struct {
	// section is the TOML section name. Empty string means root-level keys.
	section string

	// strategy determines whether to merge individual keys or replace the whole section.
	strategy mergeStrategy

	// keys lists the individual keys to merge (only used with mergeKeys strategy).
	keys []string
}

// managedSections defines all operator-controlled TOML sections and their merge behavior.
// Any key or section NOT listed here belongs to ZeroClaw and is never overwritten.
var managedSections = []managedSection{
	{section: "", strategy: mergeKeys, keys: []string{"default_provider", "default_model", "default_temperature"}},
	{section: "autonomy", strategy: mergeKeys, keys: []string{"level", "workspace_only", "allowed_commands", "forbidden_paths", "auto_approve", "always_ask"}},
	{section: "http_request", strategy: mergeKeys, keys: []string{"enabled", "allowed_domains", "max_response_size", "timeout_secs", "allow_private_hosts"}},
	{section: "web_fetch", strategy: mergeKeys, keys: []string{"enabled", "allowed_domains", "blocked_domains", "max_response_size", "timeout_secs"}},
	{section: "web_search", strategy: mergeKeys, keys: []string{"enabled", "provider", "brave_api_key", "searxng_instance_url", "max_results", "timeout_secs"}},
	{section: "gateway", strategy: mergeKeys, keys: []string{"port", "host", "allow_public_bind", "require_pairing"}},
	{section: "mcp", strategy: mergeKeys, keys: []string{"enabled", "deferred_loading", "servers"}},
	{section: "agents", strategy: replaceSection},
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
