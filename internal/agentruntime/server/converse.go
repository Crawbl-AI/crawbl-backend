package server

import (
	"errors"
	"fmt"
	"io"
	"log/slog"

	"google.golang.org/genai"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	runtimev1 "github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/proto/v1"
	"github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/runner"
)

// converseHandler implements runtimev1.AgentRuntimeServer.Converse as a
// bidi stream that forwards each ConverseRequest to the ADK runner and
// translates each session.Event yielded back into a ConverseEvent oneof.
//
// This is the hot path of the runtime: every user turn flows through
// here. It replaces the iteration-3 agentRuntimeStub which returned
// codes.Unimplemented.
type converseHandler struct {
	runtimev1.UnimplementedAgentRuntimeServer
	logger *slog.Logger
	runner *runner.Runner
}

// newConverseHandler wires the handler against an already-constructed
// runner.Runner. main.go calls this after building the runner; the
// gRPC server wraps the result in a chain that includes the HMAC
// interceptor already defined in interceptor_auth.go.
func newConverseHandler(logger *slog.Logger, r *runner.Runner) *converseHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &converseHandler{logger: logger, runner: r}
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
// context carries a validated Principal. We use the Principal's
// workspace_id and user_id as the authoritative identity; any
// workspace_id / user_id carried in the request body is ignored for
// security (mirrors the resolveWorkspaceID helper used by the memory
// service).
func (h *converseHandler) Converse(stream runtimev1.AgentRuntime_ConverseServer) error {
	if h == nil || h.runner == nil {
		return status.Error(codes.Unavailable, "converse: runner not initialized")
	}
	ctx := stream.Context()
	principal, ok := PrincipalFromContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "converse: missing authenticated principal")
	}
	h.logger.Info("converse stream opened", "user_id", principal.UserID, "workspace_id", principal.WorkspaceID)

	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			h.logger.Info("converse stream closed by client", "user_id", principal.UserID)
			return nil
		}
		if err != nil {
			return err
		}

		sessionID := req.GetSessionId()
		message := req.GetMessage()
		if message == "" {
			if sendErr := sendError(stream, sessionID, codes.InvalidArgument, "converse: message is required"); sendErr != nil {
				return sendErr
			}
			continue
		}
		if sessionID == "" {
			// Fall back to workspace ID as session ID. Not ideal —
			// a workspace can have multiple concurrent conversations —
			// but the current orchestrator always passes a real
			// session ID, so this is a safety net, not the happy path.
			sessionID = principal.WorkspaceID
		}

		// Drive one turn through the ADK runner. RunTurn returns an
		// iterator; each yielded event gets translated into a
		// ConverseEvent oneof and sent to the client. The terminal
		// DoneEvent aggregates turns (for wire compat with ZeroClaw's
		// multi-agent response shape) and closes this turn, but the
		// outer for-loop keeps the stream open for the next request.
		turnErr := h.runOneTurn(stream, principal, sessionID, req.GetSystemPrompt(), message)
		if turnErr != nil {
			return turnErr
		}
	}
}

// runOneTurn drives a single user message through the runner and
// writes the resulting events to the stream. On terminal error it
// returns the error so Converse tears down the stream; on benign
// per-turn errors (e.g. invalid input) it sends an error ConverseEvent
// synthesized via sendError and returns nil so the stream stays open.
func (h *converseHandler) runOneTurn(
	stream runtimev1.AgentRuntime_ConverseServer,
	principal Principal,
	sessionID string,
	systemPrompt string,
	message string,
) error {
	ctx := stream.Context()
	var (
		turns      []*runtimev1.Turn
		modelName  string
		finalSeen  bool
		finalAgent string
		iterErr    error
	)
	for event, err := range h.runner.RunTurn(ctx, principal.UserID, sessionID, systemPrompt, message) {
		if err != nil {
			iterErr = err
			break
		}
		if event == nil {
			continue
		}
		// Track model name if present. ADK populates it on the final
		// LLM response for each agent; we overwrite so the last
		// agent's model is reported in the DoneEvent.
		if mv := event.LLMResponse.ModelVersion; mv != "" {
			modelName = mv
		}
		// Walk the genai.Content parts and translate each into a
		// ConverseEvent. Order is preserved by ADK's iter.Seq2, so
		// ToolCallEvent always precedes its matching ToolResultEvent
		// and all ChunkEvents precede the terminal DoneEvent (we
		// synthesize Done ourselves after the iterator finishes).
		if event.LLMResponse.Content != nil {
			for _, part := range event.LLMResponse.Content.Parts {
				ce := translatePart(event.Author, part, event.Partial)
				if ce == nil {
					continue
				}
				if sendErr := stream.Send(ce); sendErr != nil {
					return sendErr
				}
			}
			// If this event is the agent's final (non-partial) response
			// for this turn, capture it as a Turn for the DoneEvent
			// aggregation. ADK IsFinalResponse() returns true only for
			// the authoritative "model's final answer" event.
			if event.IsFinalResponse() && event.LLMResponse.Content != nil {
				finalSeen = true
				finalAgent = event.Author
				turns = append(turns, &runtimev1.Turn{
					AgentId: event.Author,
					Text:    concatPartText(event.LLMResponse.Content),
				})
			}
		}
	}
	if iterErr != nil {
		h.logger.Error("converse: runner iterator error", "error", iterErr, "user_id", principal.UserID, "session_id", sessionID)
		return sendError(stream, sessionID, codes.Internal, fmt.Sprintf("runner: %v", iterErr))
	}

	// Synthesize the terminal DoneEvent. ZeroClaw wire compatibility:
	// the orchestrator's existing consumer expects a turns[] array in
	// the final event so multi-agent responses persist cleanly.
	done := &runtimev1.ConverseEvent{
		Event: &runtimev1.ConverseEvent_Done{
			Done: &runtimev1.DoneEvent{
				Model: modelName,
				Turns: turns,
			},
		},
	}
	if !finalSeen {
		h.logger.Warn("converse: turn completed with no final response event", "user_id", principal.UserID, "session_id", sessionID)
	} else {
		h.logger.Info("converse turn complete", "user_id", principal.UserID, "session_id", sessionID, "final_agent", finalAgent, "turns", len(turns), "model", modelName)
	}
	return stream.Send(done)
}

// translatePart maps a single genai.Part into a ConverseEvent oneof.
// Returns nil for parts that carry no relevant content (e.g. empty
// text parts, system metadata we don't surface at the wire level).
func translatePart(author string, part *genai.Part, partial bool) *runtimev1.ConverseEvent {
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
	_ = partial // reserved for future chunk/non-chunk differentiation
	return nil
}

// concatPartText joins the Text values of every text-bearing Part in a
// genai.Content. Used to build the flat text field in a Turn aggregate
// for the DoneEvent.
func concatPartText(content *genai.Content) string {
	if content == nil {
		return ""
	}
	var out []byte
	for _, p := range content.Parts {
		if p == nil || p.Text == "" || p.Thought {
			continue
		}
		out = append(out, []byte(p.Text)...)
	}
	return string(out)
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
