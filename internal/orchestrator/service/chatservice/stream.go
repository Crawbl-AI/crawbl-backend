package chatservice

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/gocraft/dbr/v2"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// streamSession owns all mutable state for one callAgentStreaming invocation.
// Created at the start of the streaming call, methods on this struct replace
// the 7-8 parameter functions that previously threaded context through the pipeline.
type streamSession struct {
	ctx          context.Context
	svc          *service
	sess         *dbr.Session
	wsID         string
	convID       string
	conversation *orchestrator.Conversation
	primary      *orchestrator.Agent
	lookups      agentLookups
	placeholder  *orchestrator.Message
	streams      map[string]*subAgentStream
	pending      map[string]pendingToolCall
	log          *slog.Logger

	// User message tracking (moved from SendMessageOpts mutation).
	userMessageID string
	localID       string
	deliveredOnce *sync.Once
	readOnce      *sync.Once
	onPersisted   func(*orchestrator.Message)

	// Stream metrics.
	startTime   time.Time
	totalChunks int
	globalDone  bool
	firstChunk  bool
}

func newStreamSession(
	ctx context.Context,
	svc *service,
	opts *orchestratorservice.SendMessageOpts,
	conv *orchestrator.Conversation,
	agent *orchestrator.Agent,
	lookups agentLookups,
	placeholder *orchestrator.Message,
) *streamSession {
	return &streamSession{
		ctx:          ctx,
		svc:          svc,
		sess:         opts.Sess,
		wsID:         opts.WorkspaceID,
		convID:       conv.ID,
		conversation: conv,
		primary:      agent,
		lookups:      lookups,
		placeholder:  placeholder,
		streams:      map[string]*subAgentStream{agent.ID: {agent: agent, placeholder: placeholder, firstChunk: true}},
		pending:      make(map[string]pendingToolCall),
		log:          slog.With("agent", agent.Slug, "conv", conv.ID),

		userMessageID: opts.UserMessageID,
		localID:       opts.LocalID,
		deliveredOnce: opts.StatusDeliveredOnce,
		readOnce:      opts.StatusReadOnce,
		onPersisted:   opts.OnPersisted,

		startTime:  time.Now(),
		firstChunk: true,
	}
}

// callAgentStreaming handles a single agent's streaming gRPC call.
// Creates a placeholder, reads chunks from the runtime, emits Socket.IO
// events, and persists the final message(s). Multi-agent: distinct agent_id
// values in chunks get separate placeholder messages.
func (s *service) callAgentStreaming(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	conversation *orchestrator.Conversation,
	runtimeState *orchestrator.RuntimeStatus,
	agent *orchestrator.Agent,
	lookups agentLookups,
	extraContext string,
) ([]*orchestrator.Message, *merrors.Error) {
	wsID := opts.WorkspaceID
	convID := conversation.ID

	// 1. Emit thinking + create placeholder.
	var log *slog.Logger
	if agent != nil {
		log = slog.With("agent", agent.Slug, "conv", convID)
		s.broadcaster.EmitAgentStatus(ctx, wsID, agent.ID, string(orchestrator.AgentStatusThinking), convID)
	} else {
		log = slog.With("conv", convID)
	}

	placeholder := s.newPlaceholder(conversation.ID, agent)
	if mErr := s.savePlaceholder(ctx, opts.Sess, placeholder); mErr != nil {
		s.broadcaster.EmitAgentStatus(ctx, wsID, agent.ID, string(orchestrator.AgentStatusError))
		return nil, mErr
	}

	// 2. Open gRPC stream.
	streamCh, mErr := s.runtimeClient.SendTextStream(ctx, &userswarmclient.SendTextOpts{
		Runtime:   runtimeState,
		Message:   runtimeMessage(normalizeRuntimeMessage(opts.Content.Text, opts.Mentions), extraContext),
		SessionID: convID,
		AgentID:   runtimeAgentID(agent),
	})
	if mErr != nil {
		log.Warn("stream open failed, removing placeholder", "error", mErr.Error())
		if delErr := s.messageRepo.DeleteByID(ctx, opts.Sess, placeholder.ID); delErr != nil {
			log.Warn("failed to delete placeholder", "error", delErr.Error())
		}
		s.broadcaster.EmitAgentStatus(ctx, wsID, agent.ID, string(orchestrator.AgentStatusError), convID)
		return nil, mErr
	}

	// 3. Create session and process stream.
	ss := newStreamSession(ctx, s, opts, conversation, agent, lookups, placeholder)
	ss.emitDelivered()
	ss.processStream(streamCh)

	// 4. Finalize.
	replies := ss.finalize()
	if len(replies) == 0 {
		return nil, nil
	}
	return replies, nil
}

// processStream reads chunks from the gRPC stream channel and dispatches each
// event to the appropriate handler method on the session.
func (ss *streamSession) processStream(ch <-chan userswarmclient.StreamChunk) {
	for chunk := range ch {
		switch chunk.Type {
		case userswarmclient.StreamEventChunk, userswarmclient.StreamEventThinking:
			ss.handleText(chunk)
		case userswarmclient.StreamEventToolCall:
			ss.resolveStream(chunk.AgentID)
			ss.handleToolCall(chunk)
		case userswarmclient.StreamEventToolResult:
			ss.handleToolResult(chunk)
		case userswarmclient.StreamEventDone:
			ss.handleDone(chunk)
		}
	}
	ss.log.Info("stream complete", "streams", len(ss.streams), "chunks", ss.totalChunks,
		"ms", time.Since(ss.startTime).Milliseconds())
}

// handleText processes a text or thinking chunk: resolves the target stream,
// emits status transitions, accumulates text, and broadcasts the chunk.
func (ss *streamSession) handleText(chunk userswarmclient.StreamChunk) {
	st := ss.resolveStream(chunk.AgentID)
	if ss.firstChunk {
		ss.emitRead()
		ss.firstChunk = false
	}
	if st.firstChunk {
		ss.svc.broadcaster.EmitAgentStatus(ss.ctx, ss.wsID, st.agent.ID, string(orchestrator.AgentStatusWriting), ss.convID)
		st.firstChunk = false
	}
	st.accumulated.WriteString(chunk.Delta)
	st.chunkCount++
	ss.totalChunks++
	ss.svc.broadcaster.EmitMessageChunk(ss.ctx, ss.wsID, realtime.MessageChunkPayload{
		MessageID: st.placeholder.ID, ConversationID: ss.convID,
		AgentID: st.agent.ID, Chunk: chunk.Delta,
	})
}

// handleDone marks streams as done when StreamEventDone is received.
func (ss *streamSession) handleDone(chunk userswarmclient.StreamChunk) {
	if chunk.AgentID == "" {
		ss.globalDone = true
		for _, st := range ss.streams {
			st.done = true
		}
	} else {
		ss.resolveStream(chunk.AgentID).done = true
		ss.globalDone = allStreamsDone(ss.streams)
	}
}

// emitDelivered marks the user message as delivered (once across parallel agents).
func (ss *streamSession) emitDelivered() {
	if ss.userMessageID == "" || ss.deliveredOnce == nil {
		return
	}
	ss.deliveredOnce.Do(func() {
		ss.svc.broadcaster.EmitMessageStatus(ss.ctx, ss.wsID, realtime.MessageStatusPayload{
			MessageID: ss.userMessageID, ConversationID: ss.convID,
			LocalID: ss.localID, Status: string(orchestrator.MessageStatusDelivered),
		})
		if mErr := ss.svc.messageRepo.UpdateStatus(ss.ctx, ss.sess, ss.userMessageID, orchestrator.MessageStatusDelivered); mErr != nil {
			slog.Warn("failed to update delivered status", "msg", ss.userMessageID, "error", mErr.Error())
		}
	})
}

// emitRead marks the user message as read (once on first chunk).
func (ss *streamSession) emitRead() {
	if ss.userMessageID == "" || ss.readOnce == nil {
		return
	}
	ss.readOnce.Do(func() {
		ss.svc.broadcaster.EmitMessageStatus(ss.ctx, ss.wsID, realtime.MessageStatusPayload{
			MessageID: ss.userMessageID, ConversationID: ss.convID,
			LocalID: ss.localID, Status: string(orchestrator.MessageStatusRead),
		})
	})
}

// resolveStream maps an agent slug from a stream chunk to the corresponding
// subAgentStream entry, creating a new sub-agent stream when a previously
// unseen agent slug appears.
func (ss *streamSession) resolveStream(slug string) *subAgentStream {
	if slug == "" || slug == ss.primary.Slug {
		return ss.streams[ss.primary.ID]
	}
	sub := ss.lookups.bySlug[slug]
	if sub == nil {
		return ss.streams[ss.primary.ID]
	}
	if st, ok := ss.streams[sub.ID]; ok {
		return st
	}
	return ss.createSubAgentStream(sub)
}
