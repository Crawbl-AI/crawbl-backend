package chatservice

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/gocraft/dbr/v2"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/queue"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/usagerepo"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// streamSession owns all mutable state for one callAgentStreaming invocation.
// Created at the start of the streaming call, methods on this struct replace
// the 7-8 parameter functions that previously threaded context through the pipeline.
type streamSession struct {
	ctx          context.Context
	svc          *Service
	sess         *dbr.Session
	wsID         string
	userID       string
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

// newStreamSessionOpts groups the inputs for newStreamSession to keep the
// parameter count within the go:S107 limit.
type newStreamSessionOpts struct {
	ctx         context.Context
	svc         *Service
	sendOpts    *orchestratorservice.SendMessageOpts
	pm          *persistedMsg
	conv        *orchestrator.Conversation
	agent       *orchestrator.Agent
	lookups     agentLookups
	placeholder *orchestrator.Message
}

func newStreamSession(o newStreamSessionOpts) *streamSession {
	return &streamSession{
		ctx:          o.ctx,
		svc:          o.svc,
		sess:         database.SessionFromContext(o.ctx),
		wsID:         o.sendOpts.WorkspaceID,
		userID:       o.sendOpts.UserID,
		convID:       o.conv.ID,
		conversation: o.conv,
		primary:      o.agent,
		lookups:      o.lookups,
		placeholder:  o.placeholder,
		streams:      map[string]*subAgentStream{o.agent.ID: {agent: o.agent, placeholder: o.placeholder, firstChunk: true}},
		pending:      make(map[string]pendingToolCall),
		log:          slog.With("agent", o.agent.Slug, "conv", o.conv.ID),

		userMessageID: o.pm.userMessageID,
		localID:       o.pm.localID,
		deliveredOnce: o.pm.deliveredOnce,
		readOnce:      o.pm.readOnce,
		onPersisted:   o.pm.onPersisted,

		startTime:  time.Now(),
		firstChunk: true,
	}
}

// callAgentStreamingOpts groups the inputs for callAgentStreaming to keep the
// parameter count within the go:S107 limit.
type callAgentStreamingOpts struct {
	ctx          context.Context
	sendOpts     *orchestratorservice.SendMessageOpts
	pm           *persistedMsg
	conversation *orchestrator.Conversation
	runtimeState *orchestrator.RuntimeStatus
	agent        *orchestrator.Agent
	lookups      agentLookups
	extraContext string
}

// callAgentStreaming handles a single agent's streaming gRPC call.
// Creates a placeholder, reads chunks from the runtime, emits Socket.IO
// events, and persists the final message(s). Multi-agent: distinct agent_id
// values in chunks get separate placeholder messages.
func (s *Service) callAgentStreaming(o callAgentStreamingOpts) ([]*orchestrator.Message, *merrors.Error) {
	if o.agent == nil {
		return nil, merrors.ErrAgentNotFound
	}

	wsID := o.sendOpts.WorkspaceID
	convID := o.conversation.ID
	sess := database.SessionFromContext(o.ctx)

	// 1. Emit thinking + create placeholder.
	log := slog.With("agent", o.agent.Slug, "conv", convID)
	s.broadcaster.EmitAgentStatus(o.ctx, wsID, o.agent.ID, string(orchestrator.AgentStatusThinking), convID)

	placeholder := s.newPlaceholder(o.conversation.ID, o.agent)
	if mErr := s.savePlaceholder(o.ctx, sess, placeholder); mErr != nil {
		s.broadcaster.EmitAgentStatus(o.ctx, wsID, o.agent.ID, string(orchestrator.AgentStatusError))
		return nil, mErr
	}

	// 2. Open gRPC stream.
	streamCh, mErr := s.runtimeClient.SendTextStream(o.ctx, &userswarmclient.SendTextOpts{
		Runtime:   o.runtimeState,
		Message:   runtimeMessage(normalizeRuntimeMessage(o.sendOpts.Content.Text, o.sendOpts.Mentions), o.extraContext),
		SessionID: convID,
		AgentID:   runtimeAgentID(o.agent),
	})
	if mErr != nil {
		log.Warn("stream open failed, removing placeholder", "error", mErr.Error())
		if delErr := s.messageRepo.DeleteByID(o.ctx, sess, placeholder.ID); delErr != nil {
			log.Warn("failed to delete placeholder", "error", delErr.Error())
		}
		s.broadcaster.EmitAgentStatus(o.ctx, wsID, o.agent.ID, string(orchestrator.AgentStatusError), convID)
		return nil, mErr
	}

	// 3. Create session and process stream.
	ss := newStreamSession(newStreamSessionOpts{
		ctx:         o.ctx,
		svc:         s,
		sendOpts:    o.sendOpts,
		pm:          o.pm,
		conv:        o.conversation,
		agent:       o.agent,
		lookups:     o.lookups,
		placeholder: placeholder,
	})
	ss.emitDelivered()
	ss.processStream(streamCh)

	// 4. Finalize.
	replies := ss.finalize()

	// Auto-ingest conversation into MemPalace memory (non-blocking).
	if len(replies) > 0 {
		agentID := o.agent.ID
		s.autoIngestConversation(o.ctx, wsID, agentID, o.sendOpts.Content.Text, replies)
	}

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
		case userswarmclient.StreamEventUsage:
			ss.handleUsage(chunk)
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

// handleUsage processes a usage event from the runtime. Increments both
// user-level (monthly period) and agent-level (lifetime) token counters.
func (ss *streamSession) handleUsage(chunk userswarmclient.StreamChunk) {
	ss.log.Info("llm usage",
		"agent", chunk.AgentID,
		"model", chunk.Model,
		"prompt_tokens", chunk.PromptTokens,
		"completion_tokens", chunk.CompletionTokens,
		"total_tokens", chunk.TotalTokens,
		"cached_tokens", chunk.CachedTokens,
		"call_sequence", chunk.CallSequence,
	)

	// Resolve message ID from the agent's placeholder so mobile knows which message to attach usage to.
	var usageMessageID string
	if st := ss.resolveStreamReadOnly(chunk.AgentID); st != nil && st.placeholder != nil {
		usageMessageID = st.placeholder.ID
	}

	// Emit real-time usage update to mobile via Socket.IO.
	ss.svc.broadcaster.EmitUsageUpdate(ss.ctx, ss.wsID, realtime.UsageUpdatePayload{
		AgentID:          chunk.AgentID,
		ConversationID:   ss.convID,
		MessageID:        usageMessageID,
		Model:            chunk.Model,
		PromptTokens:     chunk.PromptTokens,
		CompletionTokens: chunk.CompletionTokens,
		TotalTokens:      chunk.TotalTokens,
		CallSequence:     chunk.CallSequence,
	})

	if ss.svc.usageRepo == nil {
		return
	}

	period := time.Now().UTC().Format("2006-01")

	// Resolve the agent DB ID from the slug in the chunk.
	var agentDBID string
	var workspaceID string
	if st := ss.resolveStreamReadOnly(chunk.AgentID); st != nil && st.agent != nil {
		agentDBID = st.agent.ID
		workspaceID = ss.wsID
	}

	// Compute cost from pricing cache.
	var costUSD float64
	if ss.svc.pricingCache != nil {
		costUSD = ss.svc.pricingCache.Compute("", chunk.Model, "global",
			chunk.PromptTokens, chunk.CompletionTokens, chunk.CachedTokens)
	}

	// Increment user usage counter (monthly).
	if mErr := ss.svc.usageRepo.IncrementUsage(ss.ctx, ss.sess, &usagerepo.IncrementUsageOpts{
		UserID:           ss.userID,
		Period:           period,
		PromptTokens:     int64(chunk.PromptTokens),
		CompletionTokens: int64(chunk.CompletionTokens),
		TotalTokens:      int64(chunk.TotalTokens),
		CostUSD:          costUSD,
	}); mErr != nil {
		slog.Warn("failed to increment user usage", "error", mErr.Error())
	}

	// Increment agent usage counter (lifetime).
	if agentDBID != "" {
		if mErr := ss.svc.usageRepo.IncrementAgentUsage(ss.ctx, ss.sess, &usagerepo.IncrementAgentUsageOpts{
			AgentID:          agentDBID,
			WorkspaceID:      workspaceID,
			PromptTokens:     int64(chunk.PromptTokens),
			CompletionTokens: int64(chunk.CompletionTokens),
			TotalTokens:      int64(chunk.TotalTokens),
			CostUSD:          costUSD,
		}); mErr != nil {
			slog.Warn("failed to increment agent usage", "error", mErr.Error())
		}
	}

	// Publish to NATS for ClickHouse analytics pipeline.
	ss.svc.usagePublisher.Publish(ss.ctx, ss.wsID, &queue.UsageEvent{
		UserID:           ss.userID,
		WorkspaceID:      ss.wsID,
		ConversationID:   ss.convID,
		AgentID:          chunk.AgentID,
		AgentDBID:        agentDBID,
		Model:            chunk.Model,
		PromptTokens:     chunk.PromptTokens,
		CompletionTokens: chunk.CompletionTokens,
		TotalTokens:      chunk.TotalTokens,
		CachedTokens:     chunk.CachedTokens,
		CostUSD:          costUSD,
		CallSequence:     chunk.CallSequence,
		SessionID:        ss.convID,
	})
}

// resolveStreamReadOnly looks up the subAgentStream for a slug without
// creating a new one. Returns nil if not found.
func (ss *streamSession) resolveStreamReadOnly(slug string) *subAgentStream {
	if ss.primary == nil {
		return nil
	}
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
	return nil
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
	if ss.primary == nil {
		return nil
	}
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
