// Package client provides the runtime client used by the orchestrator to manage
// and communicate with per-user ZeroClaw swarm runtimes running on Kubernetes.
//
// # Overview
//
// Each user's swarm is represented by a UserSwarm custom resource (CR) in the
// Kubernetes cluster.  The operator that watches those CRs is responsible for
// spinning up the actual StatefulSet, Service, and PVC — this package's job is
// only to manage the lifecycle of the CR and to forward chat messages to the
// running pod once it is ready.
//
// There are two concrete implementations of the Client interface:
//
//   - userSwarmClient – the real implementation; talks to the Kubernetes API
//     server and to the ZeroClaw webhook endpoint inside the pod.
//   - fakeClient – a lightweight stand-in used in unit tests and local
//     development where a live cluster is not available.
//
// Callers (primarily the orchestrator's workspace service) select an
// implementation at start-up via the Driver field in Config.
package client

import (
	"context"
	"net/http"
	"time"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// defaultHTTPTimeout caps how long the orchestrator will wait for a single
// webhook call to the ZeroClaw pod to complete.  90 seconds is intentionally
// generous because LLM inference calls routed through ZeroClaw can be slow,
// but it still prevents goroutine leaks when a pod becomes unresponsive.
const defaultHTTPTimeout = 90 * time.Second

// defaultStreamHTTPTimeout caps the total duration for a streaming webhook call.
// Streaming responses are long-lived (agent tool loops can take minutes), so this
// is much longer than the synchronous webhook timeout.
const defaultStreamHTTPTimeout = 10 * time.Minute

// readyConditionType is the Kubernetes condition type the UserSwarm operator
// sets on a UserSwarm CR once the runtime pod is healthy and accepting traffic.
// Matching by string constant avoids typos when inspecting the condition list.
const readyConditionType = "Ready"

// userSwarmClient is the production implementation of Client.  It holds:
//   - a controller-runtime Kubernetes client for reading/writing UserSwarm CRs
//   - the resolved UserSwarmConfig for this deployment environment
//   - an HTTP client pre-configured with a timeout for webhook calls to pods
//
// The zero value is not usable; always construct via NewUserSwarmClient.
type userSwarmClient struct {
	client           k8sclient.Client
	config           UserSwarmConfig
	httpClient       *http.Client
	httpStreamClient *http.Client
}

// webhookRequest is the JSON body sent to the ZeroClaw pod's /webhook endpoint.
// Message is required; AgentID and SystemPrompt are optional overrides that let
// the caller target a specific agent or inject a system prompt for the turn.
// Using pointer types for the optional fields means they are omitted from the
// JSON payload entirely when not set, which keeps the wire format clean.
type webhookRequest struct {
	Message      string  `json:"message"`
	AgentID      *string `json:"agent_id,omitempty"`
	SystemPrompt *string `json:"system_prompt,omitempty"`
}

// AgentTurn represents a single agent's contribution in a multi-agent response.
type AgentTurn struct {
	AgentID string `json:"agent_id"`
	Text    string `json:"text"`
}

// webhookResponse is the JSON body returned by the ZeroClaw pod after it has
// processed the message through the agent pipeline.  Turns contains the
// ordered list of agent contributions that are surfaced back to the mobile client.
type webhookResponse struct {
	Turns []AgentTurn `json:"turns"`
	Model string      `json:"model,omitempty"`
}

// StreamEventType distinguishes text chunks from tool activity and completion.
type StreamEventType string

const (
	StreamEventChunk      StreamEventType = "chunk"
	StreamEventThinking   StreamEventType = "thinking"
	StreamEventToolCall   StreamEventType = "tool_call"
	StreamEventToolResult StreamEventType = "tool_result"
	StreamEventDone       StreamEventType = "done"
)

// StreamChunk is a single NDJSON line from the ZeroClaw /webhook/stream response.
type StreamChunk struct {
	Type    StreamEventType `json:"type"`
	AgentID string          `json:"agent_id"`
	Delta   string          `json:"delta,omitempty"`
	Tool    string          `json:"tool,omitempty"`
	Args    string          `json:"args,omitempty"`
	Output  string          `json:"output,omitempty"`
	Model   string          `json:"model,omitempty"`
}

// MemoryEntry represents a single memory item from ZeroClaw's memory store.
type MemoryEntry struct {
	Key       string `json:"key"`
	Content   string `json:"content"`
	Category  string `json:"category,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// ListMemoriesOpts carries parameters for ListMemories.
type ListMemoriesOpts struct {
	// Runtime is the current state of the user's swarm.
	Runtime *orchestrator.RuntimeStatus
	// Category optionally filters memories by category (core, daily, conversation).
	Category string
	// Limit is the maximum number of entries to return.
	Limit int
	// Offset is the number of entries to skip.
	Offset int
}

// DeleteMemoryOpts carries parameters for DeleteMemory.
type DeleteMemoryOpts struct {
	// Runtime is the current state of the user's swarm.
	Runtime *orchestrator.RuntimeStatus
	// Key is the memory key to delete.
	Key string
}

// CreateMemoryOpts carries parameters for CreateMemory.
type CreateMemoryOpts struct {
	// Runtime is the current state of the user's swarm.
	Runtime *orchestrator.RuntimeStatus
	// Key is the memory key.
	Key string
	// Content is the memory content.
	Content string
	// Category is the memory category (core, daily, conversation).
	Category string
}

// fakeClient is the test/local-dev implementation of Client.  It never touches
// Kubernetes or any real pod — it just echoes messages back with a configurable
// prefix.  This makes it safe to run the orchestrator locally without a cluster.
type fakeClient struct {
	replyPrefix string
}

// Driver constants select which Client implementation is constructed.
// The value stored here must match what is read from the environment / config
// file that is passed into the server at start-up.
const (
	// DriverFake selects fakeClient.  Use this in unit tests and local dev runs.
	DriverFake = "fake"

	// DriverUserSwarm selects the real Kubernetes-backed userSwarmClient.
	// This is the value used in all deployed environments (dev, staging, prod).
	DriverUserSwarm = "userswarm"

	// DefaultFakeReplyPrefix is prepended to every echoed message when using
	// fakeClient and no custom FakeReplyPrefix is supplied in Config.
	DefaultFakeReplyPrefix = "Fake runtime reply"

	// DefaultRuntimeNamespace is the Kubernetes namespace where UserSwarm pods
	// are scheduled when no explicit namespace is configured.  All user runtimes
	// share this single namespace under the current shared-runtime model.
	DefaultRuntimeNamespace = "userswarms"

	// DefaultRuntimeStorageSize is the default PVC size requested for each
	// UserSwarm's persistent volume.  2 GiB is enough for ZeroClaw's local
	// model cache and conversation history in the initial deployment.
	DefaultRuntimeStorageSize = "2Gi"

	// DefaultRuntimePort is the TCP port that the ZeroClaw gateway process
	// listens on inside the pod.  The orchestrator uses this port when
	// constructing the in-cluster webhook URL.  Chosen to avoid conflicts with
	// common well-known ports.
	DefaultRuntimePort int32 = 42617

	// DefaultPollTimeout is the maximum amount of time EnsureRuntime will spend
	// waiting for a newly created UserSwarm to reach Verified=true.  After this
	// deadline the call returns ErrRuntimeNotReady so the caller can surface a
	// meaningful error to the mobile client rather than hanging indefinitely.
	DefaultPollTimeout = 60 * time.Second

	// DefaultPollInterval is how often EnsureRuntime re-checks the UserSwarm CR
	// status while waiting for Verified=true.  2 seconds balances responsiveness
	// against Kubernetes API server load.
	DefaultPollInterval = 2 * time.Second
)

// Config is the top-level configuration for the client package.  It is
// typically populated from environment variables by the orchestrator's main
// package and passed into NewUserSwarmClient or NewFakeClient.
type Config struct {
	// Driver selects the concrete implementation.  Must be one of DriverFake or
	// DriverUserSwarm.
	Driver string

	// FakeReplyPrefix is only used when Driver == DriverFake.  It customises the
	// echo prefix so test assertions can be specific without hard-coding the
	// default string.
	FakeReplyPrefix string

	// UserSwarm holds all Kubernetes-level configuration used by the real client.
	// Ignored when Driver == DriverFake.
	UserSwarm UserSwarmConfig
}

// UserSwarmConfig carries the Kubernetes and ZeroClaw runtime parameters that
// the real userSwarmClient needs to build and manage UserSwarm CRs.  Most
// fields map directly onto fields in the UserSwarm CRD spec
// (api/v1alpha1/userswarm_types.go).
type UserSwarmConfig struct {
	// RuntimeNamespace is the Kubernetes namespace into which all UserSwarm pods
	// are scheduled.  Defaults to DefaultRuntimeNamespace if left empty.
	RuntimeNamespace string

	// Image is the fully-qualified container image (including tag or digest) for
	// the ZeroClaw runtime.  Updated automatically by the ZeroClaw CI pipeline
	// via crawbl-argocd-apps when a new version is tagged.
	Image string

	// ImagePullSecretName is the name of the Kubernetes Secret (of type
	// kubernetes.io/dockerconfigjson) that grants nodes permission to pull the
	// ZeroClaw image from the private DigitalOcean container registry.
	ImagePullSecretName string

	// StorageSize is the PVC capacity request, e.g. "2Gi".  Larger values give
	// the runtime more space for conversation history and model artefacts.
	StorageSize string

	// StorageClassName pins the PVC to a specific Kubernetes StorageClass.
	// Leave empty to use the cluster default (DO Block Storage in the dev cluster).
	StorageClassName string

	// DefaultProvider is the LLM provider name (e.g. "anthropic", "openai")
	// injected into each new ZeroClaw runtime as its default.  This allows the
	// platform to control which provider is used before per-user BYOK is
	// supported.
	DefaultProvider string

	// DefaultModel is the LLM model identifier injected as the runtime default,
	// e.g. "claude-3-5-sonnet-20241022".  Works together with DefaultProvider.
	DefaultModel string

	// EnvSecretName is the name of a Kubernetes Secret whose key-value pairs are
	// injected as environment variables into the ZeroClaw pod.  The secret is
	// managed by External Secrets Operator (ESO) and typically contains LLM API
	// keys and other provider credentials.
	EnvSecretName string

	// TOMLOverrides is raw TOML that is merged into the ZeroClaw runtime
	// configuration before the pod starts.  The orchestrator always injects a
	// gateway section here (see desiredUserSwarm) to ensure the gateway binds
	// 0.0.0.0 so the orchestrator can reach it over the pod network.
	TOMLOverrides string

	// PollTimeout is how long EnsureRuntime waits for Verified=true.
	// Defaults to DefaultPollTimeout.
	PollTimeout time.Duration

	// PollInterval is how often EnsureRuntime polls during the wait loop.
	// Defaults to DefaultPollInterval.
	PollInterval time.Duration

	// Port is the TCP port the ZeroClaw gateway listens on inside the pod.
	// Defaults to DefaultRuntimePort.
	Port int32
}

// EnsureRuntimeOpts carries the parameters for EnsureRuntime.  Both UserID and
// WorkspaceID are required; WorkspaceID is used to derive the stable CR name so
// the same workspace always maps to the same UserSwarm CR.
type EnsureRuntimeOpts struct {
	// UserID is the platform user identifier stored in the UserSwarm spec so the
	// operator can label the pod for audit and routing purposes.
	UserID string

	// WorkspaceID determines the name of the UserSwarm CR via userswarmName().
	// Using workspace (not user) as the key allows future multi-workspace support
	// where one user may have more than one isolated swarm.
	WorkspaceID string

	// WaitForVerified controls whether EnsureRuntime blocks until the runtime
	// reports Verified=true (i.e. the pod is healthy and the gateway is
	// reachable) or returns immediately after the CR is created/updated.
	// Sign-up and sign-in flows set this to false and let the mobile client poll;
	// message-send flows that need the pod alive set this to true.
	WaitForVerified bool

	// AgentSettings carries per-agent configuration overrides from the
	// orchestrator database.  The key is the agent slug (e.g. "wally", "eve").
	// These are written into the UserSwarm CR's spec.config.agents map so they
	// flow through to the ZeroClaw config.toml via the webhook.
	AgentSettings map[string]AgentSettingsOverride
}

// AgentSettingsOverride holds per-agent config that flows into the CR spec.
// Each field corresponds to a column in the orchestrator's agent_settings table.
type AgentSettingsOverride struct {
	Model          string
	ResponseLength string
	AllowedTools   []string
}

// SendTextOpts carries the parameters for SendText.  All fields except
// AgentID and SystemPrompt are required for a successful call.
type SendTextOpts struct {
	// Runtime is the current state of the user's swarm, obtained from a prior
	// EnsureRuntime call.  SendText uses it to build the in-cluster webhook URL
	// and to verify the pod is healthy before attempting the call.
	Runtime *orchestrator.RuntimeStatus

	// Message is the user's raw chat message that will be forwarded to the
	// ZeroClaw agent pipeline.
	Message string

	// SessionID is an optional correlation token forwarded to the ZeroClaw pod
	// via the X-Session-Id header.  ZeroClaw uses it to maintain conversation
	// context across multiple turns.
	SessionID string

	// AgentID optionally targets a specific agent within the ZeroClaw swarm.
	// When empty the ZeroClaw runtime routes to its default agent.
	AgentID string

	// SystemPrompt optionally overrides the agent's system prompt for this turn
	// only.  Useful for product-level personas without modifying the ZeroClaw
	// configuration stored on disk.
	SystemPrompt string
}

// Client is the interface through which the rest of the orchestrator interacts
// with user swarm runtimes.  It has two implementations: the real
// userSwarmClient (for deployed environments) and fakeClient (for tests).
//
// Methods return *merrors.Error rather than the standard error interface so that
// HTTP status codes can be encoded alongside the message and propagated cleanly
// through the service and handler layers without type assertions.
type Client interface {
	// EnsureRuntime creates the UserSwarm CR if it does not exist, updates it if
	// the desired spec has drifted from the actual spec, and optionally blocks
	// until the operator marks the runtime as Verified=true.
	//
	// Called by the workspace service during sign-up, sign-in, and before any
	// message is forwarded to the pod.
	EnsureRuntime(ctx context.Context, opts *EnsureRuntimeOpts) (*orchestrator.RuntimeStatus, *merrors.Error)

	// SendText forwards a chat message to the running ZeroClaw pod via its
	// /webhook HTTP endpoint and returns the agent turns.
	//
	// The caller must first call EnsureRuntime (with WaitForVerified=true) to
	// guarantee the pod is healthy before calling SendText.
	SendText(ctx context.Context, opts *SendTextOpts) ([]AgentTurn, *merrors.Error)

	// SendTextStream forwards a chat message to the ZeroClaw pod via its
	// /webhook/stream endpoint and returns a channel of StreamChunks.
	// The channel is closed when the stream completes or the context is canceled.
	// The caller must first call EnsureRuntime (with WaitForVerified=true).
	SendTextStream(ctx context.Context, opts *SendTextOpts) (<-chan StreamChunk, *merrors.Error)

	// DeleteRuntime removes the UserSwarm CR for a workspace, triggering
	// the operator to clean up all child resources (StatefulSet, PVC, etc.).
	DeleteRuntime(ctx context.Context, workspaceID string) *merrors.Error

	// ListMemories retrieves all memories from the ZeroClaw runtime's memory store.
	ListMemories(ctx context.Context, opts *ListMemoriesOpts) ([]MemoryEntry, *merrors.Error)

	// DeleteMemory removes a specific memory entry by key from the ZeroClaw runtime.
	DeleteMemory(ctx context.Context, opts *DeleteMemoryOpts) *merrors.Error

	// CreateMemory stores a new memory entry in the ZeroClaw runtime.
	CreateMemory(ctx context.Context, opts *CreateMemoryOpts) *merrors.Error
}
