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
// namespace key for session storage and telemetry. We pin "crawbl"
// here so every session row and event is tagged consistently across
// restarts and shared Redis keyspaces.
const AppName = "crawbl"

// Runner is the crawbl-agent-runtime's wrapper around an ADK runner.
// It owns the constructed agent graph, one ADK runner per agent
// (sharing the injected session.Service), and exposes a single
// RunTurn entry point that routes each turn to the agent named by the
// Converse request.
//
// Runner is safe to reuse across concurrent Converse streams:
// adkrunner.Runner itself is concurrency-safe; session state is keyed
// by (userID, sessionID), so two users streaming at the same time get
// independent rows even when the same per-agent runner serves both.
type Runner struct {
	logger     *slog.Logger
	graph      *Graph
	rootRunner *adkrunner.Runner
	byAgent    map[string]*adkrunner.Runner
	// sess is the durable session service (Redis-backed in production)
	// shared across every per-agent runner. Close() calls the service's
	// Close so main.go can tear it down cleanly on shutdown.
	sess adksession.Service
}

// BuildOptions carries the already-constructed dependencies that New
// needs. Passing them in explicitly instead of building them here
// keeps the runner package free of direct LLM SDK / MCP / storage
// imports — main.go wires everything once and hands it over.
type BuildOptions struct {
	// Model is the LLM adapter constructed by model.NewFromConfig.
	Model adkmodel.LLM
	// MCPToolset is the orchestrator-mediated tool bridge. May be nil
	// for integration environments that do not exercise orchestrator
	// tools.
	MCPToolset adktool.Toolset
	// SessionService is the durable session service (Redis-backed in
	// production). Required — every per-agent runner shares this
	// instance so session history is a single conversation regardless
	// of which agent handles each turn.
	SessionService adksession.Service
	// Blueprint carries per-agent instruction and description text
	// sourced from the orchestrator's agents tables.
	Blueprint *WorkspaceBlueprint
	// LocalTools is the shared local tool slice (web_fetch,
	// web_search_tool, memory_store, memory_recall, memory_forget)
	// built once per pod by agents.BuildCommonTools and handed to
	// every agent constructor.
	LocalTools []adktool.Tool
	// Logger for the runner. If nil, slog.Default() is used.
	Logger *slog.Logger
}

// New constructs a Runner: builds the multi-agent graph from the
// blueprint, wires one ADK runner per agent against the injected
// session.Service so @mention routing can dispatch directly to the
// named sub-agent, and returns the assembled Runner.
func New(opts BuildOptions) (*Runner, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if opts.SessionService == nil {
		return nil, fmt.Errorf("runner: SessionService is required")
	}
	if opts.Blueprint == nil {
		return nil, fmt.Errorf("runner: Blueprint is required")
	}

	graph, err := BuildGraph(opts.Model, opts.MCPToolset, opts.Blueprint, opts.LocalTools)
	if err != nil {
		return nil, fmt.Errorf("runner: build graph: %w", err)
	}

	// newAgentRunner is a small helper so the error wrapping stays
	// uniform across the per-agent runner constructions.
	newAgentRunner := func(agent adkagent.Agent) (*adkrunner.Runner, error) {
		return adkrunner.New(adkrunner.Config{
			AppName:           AppName,
			Agent:             agent,
			SessionService:    opts.SessionService,
			AutoCreateSession: true,
		})
	}

	rootRunner, err := newAgentRunner(graph.Root)
	if err != nil {
		return nil, fmt.Errorf("runner: construct root adk runner: %w", err)
	}

	// Per-agent runners: Manager, Wally, Eve. Each gets its own ADK
	// runner so RunTurn can dispatch based on the Converse request's
	// agent_id field. We register under the agent's structural name
	// so the orchestrator's wire value ("wally", "eve") matches how
	// the underlying ADK agent is named.
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
		sess:       opts.SessionService,
	}, nil
}

// RunTurn feeds a single user message through the agent graph and
// returns an event stream. The caller iterates the returned iter.Seq2
// and translates each session.Event into whatever wire format they
// serve — server/converse.go performs that translation for the gRPC
// bidi stream.
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
// auto-creates the session row if it does not exist yet because the
// runner was built with AutoCreateSession=true.
//
// systemPrompt, when non-empty, is injected as a per-turn prefix on
// the user message since ADK's llmagent.Config.Instruction is set at
// construction time. Orchestrator callers rarely override system
// prompts today, but the mechanism stays open for product personas.
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
	if targetAgent != "" {
		if ar, ok := r.byAgent[targetAgent]; ok {
			inner = ar
		} else {
			r.logger.Warn("runner: unknown target agent, routing to root",
				"requested", targetAgent,
				"known_agents", r.knownAgentNames(),
				"session_id", sessionID,
			)
		}
	}

	// Build the user message as a genai.Content value. ADK expects a
	// single *genai.Content per turn — a "user" role content with the
	// message text as a single part.
	text := message
	if systemPrompt != "" {
		text = "[system]\n" + systemPrompt + "\n[/system]\n\n" + message
	}
	content := genai.NewContentFromText(text, genai.RoleUser)

	runCfg := adkagent.RunConfig{
		// StreamingMode=Sse is the ADK value for "yield partial chunks
		// plus the final complete event". Matches the gRPC bidi
		// semantics the ConverseEvent oneof expects.
		StreamingMode: adkagent.StreamingMode("sse"),
	}
	return inner.Run(ctx, userID, sessionID, content, runCfg)
}

// Close releases the session service backing this runner. Safe to
// call multiple times. main.go calls Close from server.Shutdown after
// the gRPC server has drained so in-flight turns finish before the
// Redis connection closes.
func (r *Runner) Close() error {
	if r == nil || r.sess == nil {
		return nil
	}
	if closer, ok := r.sess.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

// RootAgentName returns the name of the root agent. Used by the
// Converse handler to tag ChunkEvents that come from the root itself
// (not a delegated sub-agent) with the correct agent_id.
func (r *Runner) RootAgentName() string {
	if r == nil || r.graph == nil || r.graph.Root == nil {
		return ""
	}
	return r.graph.Root.Name()
}

// knownAgentNames returns the list of agent names the runner can
// dispatch to. Exposed privately for log enrichment so unknown-target
// warnings name the valid alternatives.
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
// events. Used by RunTurn when preconditions fail so callers can
// treat error propagation uniformly with normal event streams.
func errIter(err error) iter.Seq2[*adksession.Event, error] {
	return func(yield func(*adksession.Event, error) bool) {
		yield(nil, err)
	}
}
