package server

import (
	"context"
	"log/slog"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"

	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/config"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/runner"
	runtimev1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/agentruntime/v1"
	crawblgrpc "github.com/Crawbl-AI/crawbl-backend/internal/pkg/grpc"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/telemetry"
)

// gRPC server tuning constants. Named to satisfy the mnd linter.
const (
	serverMaxConnectionIdle     = 5 * time.Minute
	serverMaxConnectionAge      = 30 * time.Minute
	serverMaxConnectionAgeGrace = 10 * time.Second
	serverKeepaliveTime         = 30 * time.Second
	serverKeepaliveTimeout      = 10 * time.Second
	serverMinKeepaliveTime      = 15 * time.Second
	serverMaxConcurrentStreams  = 32
)

// Log preview length constants for structured log fields.
const (
	// previewLenArgs is the max runes for tool-call args previews.
	previewLenArgs = 120
	// previewLenMessage is the max runes for user message previews.
	previewLenMessage = 120
	// previewLenReply is the max runes for agent reply previews.
	previewLenReply = 160
)

// Server is the top-level gRPC server wrapper for crawbl-agent-runtime.
// It owns the net.Listener, the *grpc.Server, the HealthServer, and
// holds a reference to the injected runner so Shutdown can tear it
// down in the correct order. main.go constructs one Server via New(),
// calls Start() in a goroutine, and Shutdown() on SIGTERM.
//
// Every piece of generic gRPC infrastructure (HMAC auth interceptor,
// graceful shutdown, PerRPC credentials symmetry with the client)
// lives in internal/pkg/grpc. This package only contains
// agentruntime-specific wiring: the Server struct, the Converse
// service handler, and the HealthServer lifecycle.
type Server struct {
	cfg      config.Config
	logger   *slog.Logger
	listener net.Listener
	grpcSrv  *grpc.Server
	health   *HealthServer
	runner   *runner.Runner

	// lifecycleCancel cancels the context passed to net.Listen in Start,
	// bounding the listener bring-up to the Server's own lifetime.
	// Cancelled on Shutdown so an in-progress net.Listen call (e.g. on a
	// slow DNS resolution) unblocks cleanly instead of hanging forever.
	lifecycleCancel context.CancelFunc
}

// Deps bundles the dependencies main.go constructs before calling New.
// Passing them through a single struct keeps the server package free
// of direct Redis / model imports and makes the dependency graph
// obvious at the wiring site.
type Deps struct {
	// Runner drives Converse turns. Required.
	Runner *runner.Runner
	// Logger for the server. If nil, slog.Default() is used.
	Logger *slog.Logger
}

// HealthServer is a thin wrapper around google.golang.org/grpc/health so the
// rest of the runtime never imports the grpc health package directly. Two
// states are exposed: NotServing (default at startup, before the agent graph
// is loaded) and Serving (after the runner is ready).
//
// The wrapper is deliberately minimal so US-AR-003 can ship with the server
// skeleton; US-AR-009 uses it to flip state once the ADK runner is wired.
type HealthServer struct {
	inner *grpcHealthServer
}

// converseHandler implements runtimev1.AgentRuntimeServer.Converse as
// a bidi stream that forwards each ConverseRequest to the ADK runner
// and translates each session.Event yielded back into a ConverseEvent
// oneof. This is the hot path of the runtime — every user turn flows
// through here.
type converseHandler struct {
	runtimev1.UnimplementedAgentRuntimeServer
	logger  *slog.Logger
	runner  *runner.Runner
	metrics *telemetry.TurnMetrics
}

// turnState accumulates per-turn observations (model version, final
// text, tool calls, partial chunks, authoring agents) so the single
// "turn complete" log line at the end carries the full story.
type turnState struct {
	targetAgent  string
	modelName    string
	finalAgent   string
	finalText    string
	finalSeen    bool
	partialCount int
	toolCalls    []string
	authors      map[string]int
	callSequence int32
}

// drainOpts groups the inputs for drainRunnerEvents.
type drainOpts struct {
	stream       runtimev1.AgentRuntime_ConverseServer
	principal    crawblgrpc.Identity
	sessionID    string
	systemPrompt string
	targetAgent  string
	message      string
	state        *turnState
}

// turnDoneOpts groups the inputs for sendTurnDone.
type turnDoneOpts struct {
	stream      runtimev1.AgentRuntime_ConverseServer
	principal   crawblgrpc.Identity
	sessionID   string
	targetAgent string
	message     string
	state       *turnState
	turns       []*runtimev1.Turn
	start       time.Time
}

// grpcHealthServer aliases the upstream health server type so the rest of
// server/ can refer to it without importing the grpc/health package.
type grpcHealthServer = health.Server
