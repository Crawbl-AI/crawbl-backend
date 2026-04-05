package runner

import (
	"context"
	"fmt"
	"iter"
	"log/slog"

	"google.golang.org/genai"

	adkagent "google.golang.org/adk/agent"
	adkmodel "google.golang.org/adk/model"
	adkrunner "google.golang.org/adk/runner"
	adksession "google.golang.org/adk/session"
	adktool "google.golang.org/adk/tool"
)

// AppName is the ADK runner's AppName parameter. ADK uses it as a
// namespace key for session storage + telemetry. We pin "crawbl" here
// so every session row/event is tagged consistently across restarts.
const AppName = "crawbl"

// Runner is the crawbl-agent-runtime's wrapper around an ADK runner.
// It owns the constructed agent graph, the session service, and the
// underlying *adkrunner.Runner. main.go builds one Runner at startup
// and hands it to the gRPC server; every Converse turn flows through
// Runner.Run, which streams ADK events back to the caller via an
// iter.Seq2 — the caller is responsible for translating events into
// whatever wire format it serves (our Converse handler translates to
// proto).
//
// The Runner is safe to reuse across concurrent Converse streams
// because adkrunner.Runner itself is concurrent-safe; session state
// is keyed by (userID, sessionID), so two users streaming at the same
// time get independent session rows.
type Runner struct {
	logger *slog.Logger
	graph  *Graph
	inner  *adkrunner.Runner
	// sess is held so Close() (Phase 2) can tear it down cleanly.
	sess adksession.Service
}

// BuildOptions carries the already-constructed dependencies that New
// needs. Passing them in explicitly instead of building them here keeps
// the runner package free of direct LLM SDK / MCP imports — the main
// package wires everything once and hands it over.
type BuildOptions struct {
	// Model is the LLM adapter (Phase 1: OpenAI via adk-utils-go).
	Model adkmodel.LLM
	// MCPToolset is the orchestrator-mediated tool bridge. May be nil
	// for tests that don't exercise orchestrator tools.
	MCPToolset adktool.Toolset
	// Logger for the runner. If nil, slog.Default() is used.
	Logger *slog.Logger
}

// New constructs a Runner: builds the three-agent graph, creates an
// in-memory session service, and wires both into an ADK runner. The
// session service is in-memory for Phase 1 (lost on pod restart, same
// trade-off as memory.InMemoryStore). Phase 2 swaps in a Postgres-
// backed session service when the migrations land.
func New(opts BuildOptions) (*Runner, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	graph, err := BuildGraph(opts.Model, opts.MCPToolset)
	if err != nil {
		return nil, fmt.Errorf("runner: build graph: %w", err)
	}

	sessionService := adksession.InMemoryService()
	inner, err := adkrunner.New(adkrunner.Config{
		AppName:           AppName,
		Agent:             graph.Root,
		SessionService:    sessionService,
		AutoCreateSession: true,
	})
	if err != nil {
		return nil, fmt.Errorf("runner: construct adk runner: %w", err)
	}

	return &Runner{
		logger: logger,
		graph:  graph,
		inner:  inner,
		sess:   sessionService,
	}, nil
}

// RunTurn feeds a single user message through the agent graph and
// returns an event stream. The caller iterates the returned iter.Seq2
// and translates each session.Event into whatever wire format they
// serve — for crawbl-agent-runtime that translation happens in
// server/converse.go.
//
// sessionID is client-supplied (from the gRPC ConverseRequest); ADK
// auto-creates the session row if it doesn't exist yet because we
// passed AutoCreateSession=true in New.
//
// systemPrompt, when non-empty, is injected as a GlobalInstruction-
// equivalent by prepending a user-role turn to the message — Phase 1
// shortcut because ADK llmagent's Instruction field is set at config
// time, not per-turn. The orchestrator rarely overrides system prompts
// today, so this trade-off is acceptable for the POC.
func (r *Runner) RunTurn(ctx context.Context, userID, sessionID, systemPrompt, message string) iter.Seq2[*adksession.Event, error] {
	if r == nil || r.inner == nil {
		return errIter(fmt.Errorf("runner: not initialized"))
	}
	if userID == "" || sessionID == "" || message == "" {
		return errIter(fmt.Errorf("runner: userID, sessionID, and message are required"))
	}

	// Build the user message as a genai.Content value. ADK expects a
	// single *genai.Content per turn — a "user" role content with the
	// message text as a single part.
	text := message
	if systemPrompt != "" {
		// Inline the per-turn system prompt override. Crude but
		// correct for Phase 1 — the orchestrator historically uses
		// system_prompt overrides rarely and only for product personas.
		text = "[system]\n" + systemPrompt + "\n[/system]\n\n" + message
	}
	content := genai.NewContentFromText(text, genai.RoleUser)

	runCfg := adkagent.RunConfig{
		// StreamingMode=Sse is the ADK value for "yield partial chunks
		// plus the final complete event". Matches the gRPC bidi
		// semantics our ConverseEvent oneof expects.
		StreamingMode: adkagent.StreamingMode("sse"),
	}
	return r.inner.Run(ctx, userID, sessionID, content, runCfg)
}

// RootAgentName returns the name of the root agent (always "manager"
// in Phase 1). Used by the Converse handler to tag ChunkEvents that
// come from the root itself (not a delegated sub-agent) with the
// correct agent_id.
func (r *Runner) RootAgentName() string {
	if r == nil || r.graph == nil || r.graph.Root == nil {
		return ""
	}
	return r.graph.Root.Name()
}

// errIter returns an iter.Seq2 that yields a single error and no
// events. Used by RunTurn when preconditions fail so callers can treat
// error propagation uniformly with normal event streams.
func errIter(err error) iter.Seq2[*adksession.Event, error] {
	return func(yield func(*adksession.Event, error) bool) {
		yield(nil, err)
	}
}
