package client

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"time"

	crawblgrpc "github.com/Crawbl-AI/crawbl-backend/internal/pkg/grpc"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"

	runtimev1 "github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/proto/v1"
)

// SendText forwards a user's chat message to the runtime pod via the
// Converse bidi stream and returns the aggregated agent turns.
//
// Even though Converse is a bidi stream, SendText is a one-shot
// interaction: send a single ConverseRequest, drain events until the
// terminal DoneEvent, return. For multi-turn live streaming use
// SendTextStream.
//
// Auth: the returned connection carries HMACCredentials at dial time,
// so we only need to stamp the caller's (UserID, WorkspaceID) onto the
// request ctx via crawblgrpc.WithAuthIdentity before opening the stream.
// Every RPC on the stream automatically attaches the signed bearer.
func (c *userSwarmClient) SendText(ctx context.Context, opts *SendTextOpts) ([]AgentTurn, *merrors.Error) {
	if opts == nil || opts.Runtime == nil || strings.TrimSpace(opts.Message) == "" {
		return nil, merrors.ErrInvalidInput
	}
	if !opts.Runtime.Verified || strings.TrimSpace(opts.Runtime.ServiceName) == "" || strings.TrimSpace(opts.Runtime.RuntimeNamespace) == "" {
		return nil, merrors.ErrRuntimeNotReady
	}
	if strings.TrimSpace(opts.Runtime.UserID) == "" || strings.TrimSpace(opts.Runtime.WorkspaceID) == "" {
		return nil, merrors.NewServerErrorText("runtime missing identity (EnsureRuntime must stamp UserID + WorkspaceID)")
	}

	conn, err := c.conn(ctx, &runtimeCoord{
		serviceName: opts.Runtime.ServiceName,
		namespace:   opts.Runtime.RuntimeNamespace,
	})
	if err != nil {
		return nil, wrapGRPCError(err, "dial runtime")
	}

	authedCtx := crawblgrpc.WithIdentity(ctx, crawblgrpc.Identity{
		Subject: opts.Runtime.UserID,
		Object:  opts.Runtime.WorkspaceID,
	})

	client := runtimev1.NewAgentRuntimeClient(conn)
	stream, err := client.Converse(authedCtx)
	if err != nil {
		return nil, wrapGRPCError(err, "open converse stream")
	}

	req := &runtimev1.ConverseRequest{
		SessionId:    opts.SessionID,
		Message:      opts.Message,
		AgentId:      opts.AgentID,
		SystemPrompt: opts.SystemPrompt,
		WorkspaceId:  opts.Runtime.WorkspaceID,
		UserId:       opts.Runtime.UserID,
	}
	if sendErr := stream.Send(req); sendErr != nil {
		return nil, wrapGRPCError(sendErr, "send converse request")
	}
	if closeErr := stream.CloseSend(); closeErr != nil {
		return nil, wrapGRPCError(closeErr, "close send")
	}

	var turns []AgentTurn
	for {
		event, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			return nil, wrapGRPCError(recvErr, "recv converse event")
		}
		if done := event.GetDone(); done != nil {
			for _, t := range done.GetTurns() {
				if strings.TrimSpace(t.GetText()) == "" {
					continue
				}
				turns = append(turns, AgentTurn{
					AgentID: t.GetAgentId(),
					Text:    t.GetText(),
				})
			}
			break
		}
		// Chunk / Thinking / ToolCall / ToolResult events are dropped in
		// the unary SendText path. SendTextStream surfaces them for
		// live streaming consumers.
	}

	if len(turns) == 0 {
		return nil, merrors.NewServerErrorText("runtime returned no turns")
	}
	return turns, nil
}

// SendTextStream opens a Converse bidi stream and returns a channel of
// translated StreamChunk events. The channel closes when the runtime
// sends its DoneEvent, the context is canceled, or the stream fails.
func (c *userSwarmClient) SendTextStream(ctx context.Context, opts *SendTextOpts) (<-chan StreamChunk, *merrors.Error) {
	if opts == nil || opts.Runtime == nil || strings.TrimSpace(opts.Message) == "" {
		return nil, merrors.ErrInvalidInput
	}
	if !opts.Runtime.Verified || strings.TrimSpace(opts.Runtime.ServiceName) == "" || strings.TrimSpace(opts.Runtime.RuntimeNamespace) == "" {
		return nil, merrors.ErrRuntimeNotReady
	}
	if strings.TrimSpace(opts.Runtime.UserID) == "" || strings.TrimSpace(opts.Runtime.WorkspaceID) == "" {
		return nil, merrors.NewServerErrorText("runtime missing identity (EnsureRuntime must stamp UserID + WorkspaceID)")
	}

	conn, err := c.conn(ctx, &runtimeCoord{
		serviceName: opts.Runtime.ServiceName,
		namespace:   opts.Runtime.RuntimeNamespace,
	})
	if err != nil {
		return nil, wrapGRPCError(err, "dial runtime")
	}

	authedCtx := crawblgrpc.WithIdentity(ctx, crawblgrpc.Identity{
		Subject: opts.Runtime.UserID,
		Object:  opts.Runtime.WorkspaceID,
	})

	client := runtimev1.NewAgentRuntimeClient(conn)
	stream, err := client.Converse(authedCtx)
	if err != nil {
		return nil, wrapGRPCError(err, "open converse stream")
	}

	req := &runtimev1.ConverseRequest{
		SessionId:    opts.SessionID,
		Message:      opts.Message,
		AgentId:      opts.AgentID,
		SystemPrompt: opts.SystemPrompt,
		WorkspaceId:  opts.Runtime.WorkspaceID,
		UserId:       opts.Runtime.UserID,
	}
	if sendErr := stream.Send(req); sendErr != nil {
		return nil, wrapGRPCError(sendErr, "send converse request")
	}
	if closeErr := stream.CloseSend(); closeErr != nil {
		return nil, wrapGRPCError(closeErr, "close send")
	}

	// Structured logging policy for this stream: one INFO at close time
	// carrying the full turn summary (duration, agent_id, chunk count).
	// Stream open is DEBUG so long-lived sessions don't flood the log
	// with noise. Errors log at WARN/ERROR with enough context to tell
	// which workspace + agent combination failed.
	streamStart := time.Now()
	slog.Debug("runtime converse stream opened",
		"workspace_id", opts.Runtime.WorkspaceID,
		"target_agent", orDefault(opts.AgentID, "<manager>"),
		"service", opts.Runtime.ServiceName,
	)

	ch := make(chan StreamChunk, 16)
	go func() {
		defer close(ch)
		var chunkCount int
		var doneSeen bool
		for {
			event, recvErr := stream.Recv()
			if errors.Is(recvErr, io.EOF) {
				if !doneSeen {
					slog.Warn("runtime converse stream closed before DoneEvent",
						"workspace_id", opts.Runtime.WorkspaceID,
						"target_agent", orDefault(opts.AgentID, "<manager>"),
						"chunks_received", chunkCount,
						"duration_ms", time.Since(streamStart).Milliseconds(),
					)
				}
				return
			}
			if recvErr != nil {
				slog.Error("runtime converse stream error",
					"workspace_id", opts.Runtime.WorkspaceID,
					"target_agent", orDefault(opts.AgentID, "<manager>"),
					"chunks_received", chunkCount,
					"duration_ms", time.Since(streamStart).Milliseconds(),
					"error", recvErr.Error(),
				)
				return
			}
			chunk, ok := translateEvent(event)
			if !ok {
				continue
			}
			chunkCount++
			select {
			case ch <- chunk:
			case <-ctx.Done():
				slog.Warn("runtime converse stream context cancelled",
					"workspace_id", opts.Runtime.WorkspaceID,
					"target_agent", orDefault(opts.AgentID, "<manager>"),
					"chunks_delivered", chunkCount,
					"duration_ms", time.Since(streamStart).Milliseconds(),
				)
				return
			}
			if chunk.Type == StreamEventDone {
				doneSeen = true
				slog.Info("runtime converse turn complete",
					"workspace_id", opts.Runtime.WorkspaceID,
					"target_agent", orDefault(opts.AgentID, "<manager>"),
					"chunks_delivered", chunkCount,
					"model", chunk.Model,
					"duration_ms", time.Since(streamStart).Milliseconds(),
				)
				return
			}
		}
	}()

	return ch, nil
}

// translateEvent maps a runtimev1.ConverseEvent oneof into the
// StreamChunk shape the orchestrator's chat service already consumes.
// Returns (zero, false) for events that carry no surfaceable content.
func translateEvent(event *runtimev1.ConverseEvent) (StreamChunk, bool) {
	if event == nil {
		return StreamChunk{}, false
	}
	switch {
	case event.GetChunk() != nil:
		c := event.GetChunk()
		return StreamChunk{
			Type:    StreamEventChunk,
			AgentID: c.GetAgentId(),
			Delta:   c.GetText(),
		}, true
	case event.GetThinking() != nil:
		t := event.GetThinking()
		return StreamChunk{
			Type:    StreamEventThinking,
			AgentID: t.GetAgentId(),
			Delta:   t.GetText(),
		}, true
	case event.GetToolCall() != nil:
		tc := event.GetToolCall()
		return StreamChunk{
			Type:    StreamEventToolCall,
			AgentID: tc.GetAgentId(),
			Tool:    tc.GetTool(),
			Args:    tc.GetArgsJson(),
			CallID:  tc.GetCallId(),
		}, true
	case event.GetToolResult() != nil:
		tr := event.GetToolResult()
		return StreamChunk{
			Type:   StreamEventToolResult,
			Output: tr.GetResultJson(),
			CallID: tr.GetCallId(),
		}, true
	case event.GetDone() != nil:
		d := event.GetDone()
		return StreamChunk{
			Type:  StreamEventDone,
			Model: d.GetModel(),
		}, true
	}
	return StreamChunk{}, false
}

// orDefault returns fallback when s is empty. Used to substitute a
// human-readable placeholder ("<manager>") in target_agent log fields
// when the caller passes an empty agent_id, so operators can tell the
// intended routing without having to know the wire contract.
func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// wrapGRPCError converts a gRPC call error into a *merrors.Error the
// orchestrator's HTTP layer can surface. Lives in this file because the
// grpc_client.go extraction to internal/pkg/grpc deliberately stays
// free of the orchestrator-specific merrors dependency.
func wrapGRPCError(err error, op string) *merrors.Error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return merrors.WrapStdServerError(err, op+": context cancelled")
	}
	return merrors.WrapStdServerError(err, op)
}
