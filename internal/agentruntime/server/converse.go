// Package server implements the gRPC AgentRuntime service handlers for the
// crawbl-agent-runtime binary. It wires the runner.Runner into the gRPC
// server and provides the Converse service handler.
package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"

	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	runtimev1 "github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/proto/v1"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/runner"
	crawblgrpc "github.com/Crawbl-AI/crawbl-backend/internal/pkg/grpc"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/telemetry"
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

// newConverseHandler wires the handler against an already-constructed
// runner.Runner. main.go calls this after building the runner; the
// gRPC server wraps the result in a chain that includes the HMAC
// interceptor already defined in crawblgrpc.NewHMACServerAuth.
func newConverseHandler(logger *slog.Logger, r *runner.Runner) *converseHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &converseHandler{
		logger:  logger,
		runner:  r,
		metrics: telemetry.NewTurnMetrics(),
	}
}

// Converse is the bidirectional streaming RPC. For each incoming
// ConverseRequest we drive a single user turn through the ADK runner
// and stream the resulting events back to the client on the same
// stream. The stream stays open across multiple turns: the client can
// send N requests and receive N terminal DoneEvents interleaved with
// streaming partial events.
//
// Authentication is already enforced by the HMAC interceptor chain
// installed in grpc_server.go — by the time Converse is invoked the
// context carries a validated crawblgrpc.Identity. The Identity's
// workspace_id and user_id are the authoritative values; any
// workspace_id / user_id carried in the request body is ignored for
// security (mirrors the resolveWorkspaceID helper used by the memory
// service).
//
// Logging contract: stream open/close is DEBUG (one per user session,
// routinely noisy on shared workspaces). Every turn produces exactly
// one INFO "turn complete" log with the summary the operator actually
// wants: target agent, final agent, message/answer previews, latency,
// model, tool call count. Tool calls and errors log in between at
// INFO/WARN/ERROR respectively.
func (h *converseHandler) Converse(stream runtimev1.AgentRuntime_ConverseServer) error {
	if h == nil || h.runner == nil {
		return status.Error(codes.Unavailable, "converse: runner not initialized")
	}
	ctx := stream.Context()
	principal, ok := crawblgrpc.IdentityFromContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "converse: missing authenticated principal")
	}
	h.logger.Debug("converse stream opened", "user_id", principal.Subject, "workspace_id", principal.Object)

	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			h.logger.Debug("converse stream closed by client", "user_id", principal.Subject, "workspace_id", principal.Object)
			return nil
		}
		if err != nil {
			return err
		}
		if err := h.dispatchConverseRequest(stream, principal, req); err != nil {
			return err
		}
	}
}

// dispatchConverseRequest validates one ConverseRequest and delegates to
// runOneTurn. Empty-message requests short-circuit with a synthetic error
// DoneEvent so the outer stream loop stays open.
func (h *converseHandler) dispatchConverseRequest(
	stream runtimev1.AgentRuntime_ConverseServer,
	principal crawblgrpc.Identity,
	req *runtimev1.ConverseRequest,
) error {
	sessionID := req.GetSessionId()
	message := req.GetMessage()
	if message == "" {
		h.logger.Warn("converse turn rejected: empty message",
			"workspace_id", principal.Object,
			"session_id", sessionID,
		)
		return sendError(stream, sessionID, codes.InvalidArgument, "converse: message is required")
	}
	if sessionID == "" {
		// Fall back to workspace ID as session ID. Not ideal — a
		// workspace can have multiple concurrent conversations —
		// but the current orchestrator always passes a real session
		// ID, so this is a safety net, not the happy path.
		sessionID = principal.Object
	}
	return h.runOneTurn(stream, principal, sessionID, req.GetSystemPrompt(), req.GetAgentId(), message)
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

func newTurnState(targetAgent string) *turnState {
	return &turnState{
		targetAgent: targetAgent,
		authors:     make(map[string]int),
	}
}

// runOneTurn drives a single user message through the runner and
// writes the resulting events to the stream. On terminal error it
// returns the error so Converse tears down the stream; on benign
// per-turn errors (e.g. invalid input) it sends an error ConverseEvent
// synthesized via sendError and returns nil so the stream stays open.
func (h *converseHandler) runOneTurn(
	stream runtimev1.AgentRuntime_ConverseServer,
	principal crawblgrpc.Identity,
	sessionID string,
	systemPrompt string,
	targetAgent string,
	message string,
) error {
	ctx := stream.Context()
	start := time.Now()
	state := newTurnState(targetAgent)

	turns, iterErr := h.streamTurnEvents(stream, principal, sessionID, systemPrompt, targetAgent, message, state)
	if iterErr != nil {
		return h.handleTurnError(ctx, stream, principal, sessionID, targetAgent, message, state, iterErr, start)
	}

	done := &runtimev1.ConverseEvent{
		Event: &runtimev1.ConverseEvent_Done{
			Done: &runtimev1.DoneEvent{
				Model: state.modelName,
				Turns: turns,
			},
		},
	}

	h.logTurnComplete(principal, sessionID, targetAgent, message, state, turns, time.Since(start))

	turnStatus := "ok"
	if !state.finalSeen {
		turnStatus = "empty"
	}
	h.metrics.Record(ctx, principal.Object, orDefault(state.finalAgent, orDefault(targetAgent, runner.AppName)), turnStatus, start)
	return stream.Send(done)
}

// streamTurnEvents drives the runner iterator, translating each event into
// wire-level ConverseEvents on stream and mutating state as it goes. It
// returns the accumulated final turns slice plus any iterator error.
func (h *converseHandler) streamTurnEvents(
	stream runtimev1.AgentRuntime_ConverseServer,
	principal crawblgrpc.Identity,
	sessionID string,
	systemPrompt string,
	targetAgent string,
	message string,
	state *turnState,
) ([]*runtimev1.Turn, error) {
	ctx := stream.Context()
	var turns []*runtimev1.Turn

	for event, err := range h.runner.RunTurn(ctx, principal.Subject, sessionID, systemPrompt, targetAgent, message) {
		if err != nil {
			return turns, err
		}
		if event == nil {
			continue
		}
		recordEventMeta(event, state)
		if event.Content == nil {
			continue
		}
		newTurns, sendErr := h.handleEventContent(stream, principal, sessionID, event, state, turns)
		if sendErr != nil {
			return turns, sendErr
		}
		turns = newTurns
	}
	return turns, nil
}

// recordEventMeta captures per-event metadata on the turn state (author,
// model version, partial count). Pure bookkeeping — no I/O.
func recordEventMeta(event *adksession.Event, state *turnState) {
	if event.Author != "" {
		state.authors[event.Author]++
	}
	// Track model name if present. ADK populates it on the final
	// LLM response for each agent; we overwrite so the last
	// agent's model is reported in the DoneEvent.
	if mv := event.ModelVersion; mv != "" {
		state.modelName = mv
	}
	if event.Partial {
		state.partialCount++
	}
}

// handleEventContent routes each content-bearing event to the partial or
// final handler. Returns a send-error (if the stream died) and the updated
// turns slice. Non-nil error means the caller should stop iterating.
func (h *converseHandler) handleEventContent(
	stream runtimev1.AgentRuntime_ConverseServer,
	principal crawblgrpc.Identity,
	sessionID string,
	event *adksession.Event,
	state *turnState,
	turns []*runtimev1.Turn,
) ([]*runtimev1.Turn, error) {
	// ADK sends content in two phases:
	//   1. Partial events (Partial=true) — streaming token-by-token.
	//   2. Final event (IsFinalResponse=true, Partial=false) — replays
	//      the complete text. We must NOT send these as ChunkEvents
	//      again or the orchestrator accumulates the text twice.
	if event.IsFinalResponse() {
		return h.handleFinalEvent(stream, event, state, turns)
	}
	return turns, h.handlePartialEvent(stream, principal, sessionID, event, state)
}

// handlePartialEvent streams each part of a partial event to the client
// and appends any FunctionCall names to state.toolCalls.
func (h *converseHandler) handlePartialEvent(
	stream runtimev1.AgentRuntime_ConverseServer,
	principal crawblgrpc.Identity,
	sessionID string,
	event *adksession.Event,
	state *turnState,
) error {
	for _, part := range event.Content.Parts {
		if part != nil && part.FunctionCall != nil && part.FunctionCall.Name != "" {
			state.toolCalls = append(state.toolCalls, part.FunctionCall.Name)
			h.logger.Info("agent tool invoked",
				"workspace_id", principal.Object,
				"session_id", sessionID,
				"agent", event.Author,
				"tool", part.FunctionCall.Name,
				"args_preview", previewMap(part.FunctionCall.Args, previewLenArgs),
			)
		}
		ce := translatePart(event.Author, part)
		if ce == nil {
			continue
		}
		if sendErr := stream.Send(ce); sendErr != nil {
			return sendErr
		}
	}
	return nil
}

// handleFinalEvent emits the UsageEvent (if metadata is present) and
// captures the aggregated Turn for the DoneEvent. Final events are not
// re-sent as chunks because they were already streamed during partials.
func (h *converseHandler) handleFinalEvent(
	stream runtimev1.AgentRuntime_ConverseServer,
	event *adksession.Event,
	state *turnState,
	turns []*runtimev1.Turn,
) ([]*runtimev1.Turn, error) {
	if err := h.maybeSendUsage(stream, event, state); err != nil {
		return turns, err
	}
	text := concatPartText(event.Content)
	if strings.TrimSpace(text) == "" {
		return turns, nil
	}
	state.finalSeen = true
	state.finalAgent = event.Author
	state.finalText = text
	return append(turns, &runtimev1.Turn{
		AgentId: event.Author,
		Text:    text,
	}), nil
}

// maybeSendUsage emits a UsageEvent for the final response when the
// adapter populated UsageMetadata. OpenAI only populates this on the
// aggregated final response, not on partial streaming chunks.
func (h *converseHandler) maybeSendUsage(
	stream runtimev1.AgentRuntime_ConverseServer,
	event *adksession.Event,
	state *turnState,
) error {
	if event.UsageMetadata == nil {
		return nil
	}
	um := event.UsageMetadata
	usageEvt := &runtimev1.ConverseEvent{
		Event: &runtimev1.ConverseEvent_Usage{
			Usage: &runtimev1.UsageEvent{
				AgentId:             event.Author,
				Model:               state.modelName,
				PromptTokens:        um.PromptTokenCount,
				CompletionTokens:    um.CandidatesTokenCount,
				TotalTokens:         um.TotalTokenCount,
				ToolUsePromptTokens: um.ToolUsePromptTokenCount,
				ThoughtsTokens:      um.ThoughtsTokenCount,
				CachedTokens:        um.CachedContentTokenCount,
				CallSequence:        state.callSequence,
			},
		},
	}
	if sendErr := stream.Send(usageEvt); sendErr != nil {
		return sendErr
	}
	state.callSequence++
	return nil
}

// handleTurnError logs the turn failure, records metrics, and sends a
// synthetic error DoneEvent to the client without tearing down the stream.
func (h *converseHandler) handleTurnError(
	ctx context.Context,
	stream runtimev1.AgentRuntime_ConverseServer,
	principal crawblgrpc.Identity,
	sessionID string,
	targetAgent string,
	message string,
	state *turnState,
	iterErr error,
	start time.Time,
) error {
	h.logger.Error("agent turn failed",
		"workspace_id", principal.Object,
		"session_id", sessionID,
		"user_id", principal.Subject,
		"target_agent", orDefault(targetAgent, "<manager>"),
		"duration_ms", time.Since(start).Milliseconds(),
		"authors_seen", authorsSlice(state.authors),
		"tool_calls", state.toolCalls,
		"message_preview", preview(message, previewLenMessage),
		"error", iterErr.Error(),
	)
	h.metrics.Record(ctx, principal.Object, orDefault(targetAgent, runner.AppName), "error", start)
	return sendError(stream, sessionID, codes.Internal, fmt.Sprintf("runner: %v", iterErr))
}

// logTurnComplete emits the single INFO/WARN line summarising the turn.
func (h *converseHandler) logTurnComplete(
	principal crawblgrpc.Identity,
	sessionID string,
	targetAgent string,
	message string,
	state *turnState,
	turns []*runtimev1.Turn,
	duration time.Duration,
) {
	baseFields := []any{
		"workspace_id", principal.Object,
		"session_id", sessionID,
		"user_id", principal.Subject,
		"target_agent", orDefault(targetAgent, "<manager>"),
		"final_agent", orDefault(state.finalAgent, "<none>"),
		"turns", len(turns),
		"partial_chunks", state.partialCount,
		"tool_calls", state.toolCalls,
		"authors_seen", authorsSlice(state.authors),
		"model", orDefault(state.modelName, "<unknown>"),
		"duration_ms", duration.Milliseconds(),
		"message_preview", preview(message, previewLenMessage),
	}
	if state.finalSeen {
		h.logger.Info("agent turn completed",
			append(baseFields, "reply_preview", preview(state.finalText, previewLenReply))...,
		)
		return
	}
	h.logger.Warn("agent turn produced no final response", baseFields...)
}

// translatePart maps a single genai.Part into a ConverseEvent oneof.
// Returns nil for parts that carry no relevant content (e.g. empty
// text parts, system metadata we don't surface at the wire level).
func translatePart(author string, part *genai.Part) *runtimev1.ConverseEvent {
	if part == nil {
		return nil
	}
	switch {
	case part.Text != "" && !part.Thought:
		return &runtimev1.ConverseEvent{
			Event: &runtimev1.ConverseEvent_Chunk{
				Chunk: &runtimev1.ChunkEvent{
					AgentId: author,
					Text:    part.Text,
				},
			},
		}
	case part.Text != "" && part.Thought:
		return &runtimev1.ConverseEvent{
			Event: &runtimev1.ConverseEvent_Thinking{
				Thinking: &runtimev1.ThinkingEvent{
					AgentId: author,
					Text:    part.Text,
				},
			},
		}
	case part.FunctionCall != nil:
		return &runtimev1.ConverseEvent{
			Event: &runtimev1.ConverseEvent_ToolCall{
				ToolCall: &runtimev1.ToolCallEvent{
					AgentId:  author,
					Tool:     part.FunctionCall.Name,
					ArgsJson: marshalArgs(part.FunctionCall.Args),
					CallId:   part.FunctionCall.ID,
				},
			},
		}
	case part.FunctionResponse != nil:
		return &runtimev1.ConverseEvent{
			Event: &runtimev1.ConverseEvent_ToolResult{
				ToolResult: &runtimev1.ToolResultEvent{
					CallId:     part.FunctionResponse.ID,
					ResultJson: marshalArgs(part.FunctionResponse.Response),
				},
			},
		}
	}
	return nil
}

// concatPartText joins the Text values of every text-bearing Part in a
// genai.Content. Used to build the flat text field in a Turn aggregate
// for the DoneEvent.
func concatPartText(content *genai.Content) string {
	if content == nil {
		return ""
	}
	var sb strings.Builder
	for _, p := range content.Parts {
		if p == nil || p.Text == "" || p.Thought {
			continue
		}
		sb.WriteString(p.Text)
	}
	return sb.String()
}

// marshalArgs converts a map[string]any into a JSON string for the
// proto ArgsJson / ResultJson fields. We use a best-effort marshal;
// on error we return an empty string so the wire never carries
// partially-formed JSON.
func marshalArgs(m map[string]any) string {
	if m == nil {
		return ""
	}
	b, err := jsonMarshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

// preview trims s to at most max runes, collapsing interior whitespace
// so a multi-line message still yields a single-line log field. Used
// for message/reply previews on every turn-complete log so operators
// can tell at a glance what a user asked and what the agent answered.
func preview(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "…"
}

// previewMap renders a best-effort single-line preview of a tool-call
// args map. Used for the "agent tool invoked" log so operators can
// tell which value a tool was called with without fetching the full
// args. Returns "{}" when the map is nil or empty.
func previewMap(m map[string]any, maxLen int) string {
	if len(m) == 0 {
		return "{}"
	}
	b, err := jsonMarshal(m)
	if err != nil {
		return "<unmarshalable>"
	}
	return preview(string(b), maxLen)
}

// authorsSlice returns the map keys sorted so log lines are stable
// across runs. Used to enumerate which agents spoke during a turn.
func authorsSlice(m map[string]int) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// orDefault returns fallback when s is empty. Used to substitute a
// human-readable placeholder in log fields that would otherwise print
// as "" (empty).
func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// sendError writes a synthetic error DoneEvent into the stream without
// tearing it down. The orchestrator's existing consumer treats any
// DoneEvent with empty turns + a Model string prefixed "ERROR:" as a
// user-visible failure for this turn.
func sendError(stream runtimev1.AgentRuntime_ConverseServer, sessionID string, code codes.Code, msg string) error {
	_ = sessionID // reserved for future per-turn error tagging
	done := &runtimev1.ConverseEvent{
		Event: &runtimev1.ConverseEvent_Done{
			Done: &runtimev1.DoneEvent{
				Model: "ERROR: " + code.String() + ": " + msg,
			},
		},
	}
	return stream.Send(done)
}
