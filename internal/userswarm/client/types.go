// Package client is the orchestrator-side client for the per-workspace
// crawbl-agent-runtime pods. It manages the UserSwarm CR lifecycle via
// the Kubernetes API (EnsureRuntime, DeleteRuntime) and forwards agent
// interactions to the running pod via gRPC (SendText, SendTextStream).
//
// The wire protocol between orchestrator and runtime is gRPC over the
// in-cluster pod network, authenticated with HMAC bearer tokens derived
// from (userID, workspaceID) using internal/pkg/hmac. The protobuf
// schema lives in proto/agentruntime/v1/ and is consumed via the
// generated stubs at internal/agentruntime/proto/v1/.
//
// There is no HTTP/NDJSON wire anywhere in this package. The legacy
// /webhook and /webhook/stream endpoints that the Rust agent runtime
// exposed are gone.
package client

import (
	"context"
	"sync"
	"time"

	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	crawblgrpc "github.com/Crawbl-AI/crawbl-backend/internal/pkg/grpc"
)

// AgentTurn represents a single agent's contribution in a multi-agent
// response. The orchestrator's chat service consumes turns and persists
// them to the messages table with the agent slug as a foreign key.
type AgentTurn struct {
	AgentID string
	Text    string
}

// StreamEventType distinguishes text chunks from tool activity and completion.
// Values are stable across the HTTP→gRPC migration because the orchestrator's
// chatservice.messages.go pattern-matches on them when translating stream
// events into realtime broadcaster events.
type StreamEventType string

const (
	StreamEventChunk      StreamEventType = "chunk"
	StreamEventThinking   StreamEventType = "thinking"
	StreamEventToolCall   StreamEventType = "tool_call"
	StreamEventToolResult StreamEventType = "tool_result"
	StreamEventDone       StreamEventType = "done"
	StreamEventUsage      StreamEventType = "usage"
)

// StreamChunk is a single streaming event the orchestrator's chat handler
// forwards to the mobile client. The runtime client translates gRPC
// ConverseEvent oneofs into StreamChunk values, preserving the shape that
// chatservice already consumes so the hot path stays stable across the
// HTTP→gRPC swap.
type StreamChunk struct {
	Type    StreamEventType
	AgentID string
	Delta   string
	Tool    string
	Args    string
	Output  string
	Model   string
	CallID  string

	// Usage fields (populated only for StreamEventUsage).
	PromptTokens        int32
	CompletionTokens    int32
	TotalTokens         int32
	ToolUsePromptTokens int32
	ThoughtsTokens      int32
	CachedTokens        int32
	CallSequence        int32
}

// Driver constants select which Client implementation is constructed.
// The value stored here must match what is read from the environment
// (CRAWBL_RUNTIME_DRIVER) before the orchestrator starts.
const (
	// DriverFake selects fakeClient. Local dev and unit tests use this
	// to run the orchestrator without a live cluster.
	DriverFake = "fake"

	// DriverCrawblRuntime selects the production Kubernetes-backed
	// client. Was DriverUserSwarm during the pre-gRPC era; renamed
	// when the HTTP webhook path was replaced with gRPC in Phase 2.
	DriverCrawblRuntime = "crawbl-runtime"

	// DefaultFakeReplyPrefix is prepended to every echoed message when
	// using fakeClient and no custom FakeReplyPrefix is supplied.
	DefaultFakeReplyPrefix = "Fake runtime reply"

	// DefaultRuntimeNamespace is the shared Kubernetes namespace where
	// every workspace pod is scheduled. Canonical value is defined in
	// api/v1alpha1.DefaultRuntimeNamespace.
	DefaultRuntimeNamespace = v1alpha1.DefaultRuntimeNamespace

	// DefaultRuntimePort is the TCP port that crawbl-agent-runtime binds
	// its gRPC server to inside the pod. Canonical value is defined in
	// api/v1alpha1.DefaultGatewayPort.
	DefaultRuntimePort int32 = v1alpha1.DefaultGatewayPort

	// DefaultPollTimeout bounds how long EnsureRuntime waits for a
	// newly created UserSwarm CR to reach Verified=true before returning
	// ErrRuntimeNotReady.
	DefaultPollTimeout = 60 * time.Second

	// DefaultPollInterval is how often EnsureRuntime re-checks the
	// UserSwarm CR status while waiting for Verified=true.
	DefaultPollInterval = 2 * time.Second
)

// Config is the top-level configuration passed into NewUserSwarmClient
// or NewFakeClient at orchestrator startup.
type Config struct {
	// Driver selects the concrete implementation: DriverFake or
	// DriverCrawblRuntime.
	Driver string

	// FakeReplyPrefix is only used when Driver == DriverFake.
	FakeReplyPrefix string

	// UserSwarm holds the Kubernetes and runtime parameters for
	// DriverCrawblRuntime.
	UserSwarm UserSwarmConfig
}

// UserSwarmConfig carries the Kubernetes + runtime parameters the
// production client needs to manage per-workspace pods and forward
// agent traffic to them over gRPC.
type UserSwarmConfig struct {
	// RuntimeNamespace is the shared Kubernetes namespace for workspace
	// pods. Defaults to DefaultRuntimeNamespace.
	RuntimeNamespace string

	// Image is the fully-qualified container image (tag or digest) for
	// the crawbl-agent-runtime binary. Updated via `crawbl app deploy
	// agent-runtime` which bumps both this value and the argocd-apps
	// webhook env var.
	Image string

	// ImagePullSecretName is the Kubernetes Secret that grants nodes
	// permission to pull the agent-runtime image from DOCR.
	ImagePullSecretName string

	// DefaultProvider is the LLM provider slug ("openai", "anthropic")
	// injected into each new workspace's runtime config.
	DefaultProvider string

	// DefaultModel is the LLM model identifier (e.g. "gpt-5-mini") used
	// as the workspace default.
	DefaultModel string

	// EnvSecretName is the name of a Kubernetes Secret whose key-value
	// pairs are injected as environment variables into the runtime pod
	// (typically LLM provider API keys managed by ESO).
	EnvSecretName string

	// MCPSigningKey is the shared HMAC secret used to sign bearer tokens
	// for gRPC calls from the orchestrator to the runtime pod AND for
	// the runtime's MCP client calls back to the orchestrator. Sourced
	// from CRAWBL_MCP_SIGNING_KEY at startup.
	MCPSigningKey string

	// RuntimeClusterKubeconfig is the path to a kubeconfig file for the
	// remote runtime cluster. When empty, the client uses in-cluster
	// config (single-cluster dev mode). When set, the client targets
	// the remote cluster for CR management and uses mTLS for gRPC.
	RuntimeClusterKubeconfig string

	// TLSCertFile is the path to the client TLS certificate for mTLS
	// gRPC connections to runtime pods in prod hybrid mode.
	TLSCertFile string

	// TLSKeyFile is the path to the client TLS private key.
	TLSKeyFile string

	// TLSCAFile is the path to the CA certificate for verifying runtime
	// pod server certificates.
	TLSCAFile string

	// PollTimeout is how long EnsureRuntime waits for Verified=true.
	PollTimeout time.Duration

	// PollInterval is how often EnsureRuntime polls during the wait loop.
	PollInterval time.Duration

	// Port is the TCP port the agent-runtime gRPC server listens on
	// inside the pod. Defaults to DefaultRuntimePort (42618).
	Port int32
}

// EnsureRuntimeOpts carries the parameters for EnsureRuntime.
type EnsureRuntimeOpts struct {
	// UserID is the platform user identifier. Stored in the UserSwarm
	// spec for per-user audit/routing, and stamped into the returned
	// RuntimeStatus so downstream gRPC calls can sign HMAC tokens.
	UserID string

	// WorkspaceID determines the CR name via userswarmName(). Also
	// stamped into the returned RuntimeStatus.
	WorkspaceID string

	// WaitForVerified controls whether EnsureRuntime blocks until the
	// runtime reports Verified=true or returns immediately after the
	// CR is created/updated.
	WaitForVerified bool

	// AgentSettings carries per-agent configuration overrides from the
	// orchestrator DB. Key is agent slug.
	AgentSettings map[string]AgentSettingsOverride
}

// AgentSettingsOverride holds per-agent config overrides that flow into
// the CR spec.
type AgentSettingsOverride struct {
	Model          string
	ResponseLength string
	AllowedTools   []string
}

// SendTextOpts carries the parameters for SendText / SendTextStream.
type SendTextOpts struct {
	// Runtime is the state returned by a prior EnsureRuntime call. Its
	// UserID and WorkspaceID fields are used to sign the HMAC bearer
	// token for the gRPC call.
	Runtime *orchestrator.RuntimeStatus

	// Message is the user's raw chat message that will be forwarded to
	// the agent pipeline.
	Message string

	// SessionID is a conversation correlation token carried in the
	// ConverseRequest.session_id proto field.
	SessionID string

	// AgentID optionally targets a specific agent within the swarm.
	// Empty routes through Manager's built-in delegation.
	AgentID string

	// SystemPrompt optionally overrides the agent's system prompt for
	// this turn only.
	SystemPrompt string
}

// Client is the interface through which the orchestrator interacts
// with user swarm runtimes. Two implementations: userSwarmClient (real,
// gRPC + Kubernetes) and fakeClient (tests).
//
// Methods return *merrors.Error so HTTP status codes can be encoded
// alongside the message and propagated through the service and handler
// layers without type assertions.
type Client interface {
	// EnsureRuntime creates the UserSwarm CR if it does not exist,
	// updates it if the desired spec has drifted, and optionally blocks
	// until the operator marks the runtime as Verified=true.
	EnsureRuntime(ctx context.Context, opts *EnsureRuntimeOpts) (*orchestrator.RuntimeStatus, *merrors.Error)

	// SendText forwards a chat message to the runtime pod via gRPC
	// Converse and returns the aggregated agent turns.
	SendText(ctx context.Context, opts *SendTextOpts) ([]AgentTurn, *merrors.Error)

	// SendTextStream opens a Converse bidi stream and returns a channel
	// of translated StreamChunk events. The channel is closed when the
	// stream completes or the context is canceled.
	SendTextStream(ctx context.Context, opts *SendTextOpts) (<-chan StreamChunk, *merrors.Error)

	// DeleteRuntime removes the UserSwarm CR for a workspace.
	DeleteRuntime(ctx context.Context, workspaceID string) *merrors.Error

	// Close releases any cached gRPC connections. Call once on
	// orchestrator shutdown.
	Close() error
}

// fakeClient is the test/local-dev implementation of Client. It never
// touches Kubernetes or any real pod — it echoes messages with a
// configurable prefix.
type fakeClient struct {
	replyPrefix string
}

// readyConditionType is the condition the UserSwarm webhook sets on a
// CR once the runtime pod passes its gRPC health check.
const readyConditionType = "Ready"

// userSwarmClient is the production implementation of Client. It owns:
//   - a controller-runtime Kubernetes client for CR management
//   - the resolved UserSwarmConfig for this deployment environment
//   - a *crawblgrpc.Pool that caches workspace gRPC connections with
//     single-flight dial, keepalive, and HMAC per-RPC credentials
//
// The zero value is not usable; always construct via NewUserSwarmClient.
type userSwarmClient struct {
	client      k8sclient.Client
	config      UserSwarmConfig
	grpcPool    *crawblgrpc.Pool
	cache       *runtimeCache
	cacheCancel context.CancelFunc
}

// runtimeCacheTTL is how long a cached RuntimeStatus is considered fresh.
// 15 seconds balances API call reduction (~95%) with status freshness.
const runtimeCacheTTL = 15 * time.Second

// sweepInterval is how often the background goroutine prunes expired entries.
const sweepInterval = 60 * time.Second

// runtimeCache is a simple TTL-based cache mapping workspace IDs to their
// last-known RuntimeStatus. It is safe for concurrent use. A background
// sweep goroutine prunes expired entries to prevent unbounded growth.
type runtimeCache struct {
	mu      sync.RWMutex
	entries map[string]runtimeCacheEntry
}

type runtimeCacheEntry struct {
	status    *orchestrator.RuntimeStatus
	expiresAt time.Time
}
