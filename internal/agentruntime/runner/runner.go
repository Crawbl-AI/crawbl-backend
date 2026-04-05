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
// It owns the constructed agent graph, one ADK runner per agent
// (shared session service), and exposes a single RunTurn entry point
// that routes each turn to the agent named by the Converse request.
//
// The Runner is safe to reuse across concurrent Converse streams
// because adkrunner.Runner itself is concurrent-safe; session state
// is keyed by (userID, sessionID), so two users streaming at the same
// time get independent session rows even when the same per-agent
// runner serves both.
type Runner struct {
	logger *slog.Logger
	graph  *Graph
	// rootRunner is the Manager-rooted runner. Used when a turn does
	// not target a specific sub-agent (the common case — the Manager
	// decides whether to delegate or answer directly).
	rootRunner *adkrunner.Runner
	// byAgent maps an agent name (e.g. "manager", "wally", "eve") to a
	// dedicated ADK runner whose root is that specific agent. Mention-
	// routing turns with a non-empty Converse agent_id select the
	// matching runner so the message bypasses the manager's delegation
	// heuristics and lands directly on the intended sub-agent. Every
	// per-agent runner shares the same in-memory session service so
	// (userID, sessionID) history is a single conversation regardless
	// of which agent handled each turn.
	byAgent map[string]*adkrunner.Runner
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
// in-memory session service, and wires one ADK runner per agent so
// @mention routing can dispatch directly to the named sub-agent.
// The session service is in-memory for Phase 1 (lost on pod restart,
// same trade-off as memory.InMemoryStore). Phase 2 swaps in a
// Postgres-backed session service when the migrations land.
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

	// newAgentRunner is a small helper so the error wrapping stays
	// uniform across the three per-agent runner constructions.
	newAgentRunner := func(agent adkagent.Agent) (*adkrunner.Runner, error) {
		return adkrunner.New(adkrunner.Config{
			AppName:           AppName,
			Agent:             agent,
			SessionService:    sessionService,
			AutoCreateSession: true,
		})
	}

	rootRunner, err := newAgentRunner(graph.Root)
	if err != nil {
		return nil, fmt.Errorf("runner: construct root adk runner: %w", err)
	}

	// Per-agent runners: Manager, Wally, Eve. Each gets its own ADK
	// runner so RunTurn can dispatch based on the Converse request's
	// agent_id field. We register under both the agent's structural
	// name (graph.Manager.Name()) and the Crawbl agent slug so the
	// orchestrator's wire value ("wally", "eve") matches regardless
	// of how the underlying ADK agent is named.
	byAgent := make(map[string]*adkrunner.Runner, 3)
	register := func(key string, agent adkagent.Agent) error {
		if key == "" || agent == nil {
			return nil
		}
		if _, dup := byAgent[key]; dup {
			return nil
		}
		ar, err := newAgentRunner(agent)
		if err != nil {
			return fmt.Errorf("runner: construct per-agent runner for %q: %w", key, err)
		}
		byAgent[key] = ar
		return nil
	}
	if err := register(graph.Manager.Name(), graph.Manager); err != nil {
		return nil, err
	}
	if err := register(graph.Wally.Name(), graph.Wally); err != nil {
		return nil, err
	}
	if err := register(graph.Eve.Name(), graph.Eve); err != nil {
		return nil, err
	}

	return &Runner{
		logger:     logger,
		graph:      graph,
		rootRunner: rootRunner,
		byAgent:    byAgent,
		sess:       sessionService,
	}, nil
}

// RunTurn feeds a single user message through the agent graph and
// returns an event stream. The caller iterates the returned iter.Seq2
// and translates each session.Event into whatever wire format they
// serve — for crawbl-agent-runtime that translation happens in
// server/converse.go.
//
// targetAgent is the authoritative routing field carried on the wire
// as ConverseRequest.agent_id:
//   - ""            → dispatch through the Manager root (Manager may
//                     answer directly or delegate to a sub-agent via
//                     ADK's transfer_to_agent flow).
//   - "wally"/"eve" → dispatch directly to that sub-agent, bypassing
//                     the Manager's delegation heuristics. The sub-
//                     agent sees the raw user message and answers
//                     without routing through a parent.
//   - unknown name  → fall back to the Manager root and log a warning.
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
func (r *Runner) RunTurn(ctx context.Context, userID, sessionID, systemPrompt, targetAgent, message string) iter.Seq2[*adksession.Event, error] {
	if r == nil || r.rootRunner == nil {
		return errIter(fmt.Errorf("runner: not initialized"))
	}
	if userID == "" || sessionID == "" || message == "" {
		return errIter(fmt.Errorf("runner: userID, sessionID, and message are required"))
	}

	// Pick the runner that serves this turn. Empty targetAgent means
	// Manager root; a named sub-agent wins iff registered, otherwise
	// we log once and fall back so unknown slugs never block traffic.
	inner := r.rootRunner
	routed := ""
	if targetAgent != "" {
		if ar, ok := r.byAgent[targetAgent]; ok {
			inner = ar
			routed = targetAgent
		} else {
			r.logger.Warn("runner: unknown target agent, routing to root",
				"requested", targetAgent,
				"known_agents", r.knownAgentNames(),
				"session_id", sessionID,
			)
		}
	}
	_ = routed // reserved for future per-runner telemetry tags

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
	return inner.Run(ctx, userID, sessionID, content, runCfg)
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

// knownAgentNames returns the sorted list of agent names the runner
// can dispatch to. Exposed privately for log enrichment so unknown-
// targetAgent warnings name the valid alternatives.
func (r *Runner) knownAgentNames() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.byAgent))
	for name := range r.byAgent {
		names = append(names, name)
	}
	return names
}

// errIter returns an iter.Seq2 that yields a single error and no
// events. Used by RunTurn when preconditions fail so callers can treat
// error propagation uniformly with normal event streams.
func errIter(err error) iter.Seq2[*adksession.Event, error] {
	return func(yield func(*adksession.Event, error) bool) {
		yield(nil, err)
	}
}
