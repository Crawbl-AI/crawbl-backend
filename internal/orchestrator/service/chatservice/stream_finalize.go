package chatservice

import (
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/gocraft/dbr/v2"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
)

// finalize persists all stream messages. Delegation (primary) is finalized
// first to guarantee correct timestamp ordering.
func (ss *streamSession) finalize() []*orchestrator.Message {
	var replies []*orchestrator.Message

	// Primary agent first when delegation occurred.
	if primarySt := ss.streams[ss.primary.ID]; primarySt != nil && len(ss.streams) > 1 {
		if reply := ss.finalizePrimaryDelegation(primarySt); reply != nil {
			replies = append(replies, reply)
		}
	}

	// Remaining streams.
	for _, st := range ss.streams {
		if st.agent.ID == ss.primary.ID && len(ss.streams) > 1 {
			continue
		}
		if reply := ss.finalizeStream(st); reply != nil {
			replies = append(replies, reply)
		}
	}

	ss.emitSubAgentDelegationDone()
	return replies
}

// finalizePrimaryDelegation finalizes the primary agent's delegation message,
// updates the summary, and emits the agent-online status event.
// Returns the persisted reply, or nil if nothing was emitted.
func (ss *streamSession) finalizePrimaryDelegation(primarySt *subAgentStream) *orchestrator.Message {
	text := strings.TrimSpace(primarySt.accumulated.String())
	delegatee := ss.firstSubAgent()

	primarySt.placeholder.Content = orchestrator.MessageContent{
		Type:        orchestrator.MessageContentTypeDelegation,
		Text:        text,
		From:        orchestrator.ContentAgentFromAgent(ss.primary),
		To:          orchestrator.ContentAgentFromAgent(delegatee),
		Status:      realtime.AgentDelegationStatusCompleted,
		TaskPreview: truncateText(text, taskPreviewMaxRunes),
	}
	reply := ss.finalizeMessage(primarySt.placeholder, text, orchestrator.MessageStatusDelegated)
	ss.svc.updateDelegationSummary(ss.placeholder.ID, text)
	ss.svc.broadcaster.EmitAgentStatus(ss.ctx, ss.wsID, ss.primary.ID, string(orchestrator.AgentStatusOnline), ss.convID)
	return reply
}

// firstSubAgent returns the sub-agent with the lexicographically smallest ID
// among all non-primary streams. Used for deterministic delegation targeting in
// multi-delegation scenarios.
func (ss *streamSession) firstSubAgent() *orchestrator.Agent {
	streamIDs := make([]string, 0, len(ss.streams))
	for id := range ss.streams {
		streamIDs = append(streamIDs, id)
	}
	sort.Strings(streamIDs)
	for _, id := range streamIDs {
		if id != ss.primary.ID {
			return ss.streams[id].agent
		}
	}
	return nil
}

// emitSubAgentDelegationDone emits delegation-completion events for all sub-agents.
func (ss *streamSession) emitSubAgentDelegationDone() {
	for _, st := range ss.streams {
		if st.agent.ID == ss.primary.ID {
			continue
		}
		ss.svc.broadcaster.EmitAgentDelegation(ss.ctx, ss.wsID, &realtime.AgentDelegationPayload{
			From:           delegationAgent(ss.primary),
			To:             delegationAgent(st.agent),
			ConversationId: ss.convID,
			Status:         realtime.AgentDelegationStatusCompleted,
			MessageId:      st.placeholder.ID,
		})
	}
}

// finalizeStream handles finalization for a single non-primary agent stream.
// Returns the persisted reply message, or nil if the placeholder was deleted (empty/silent).
func (ss *streamSession) finalizeStream(st *subAgentStream) *orchestrator.Message {
	text := strings.TrimSpace(st.accumulated.String())
	cleanDone := st.done || ss.globalDone

	// Empty — delete placeholder and emit silent done.
	if text == "" {
		if mErr := ss.svc.messageRepo.DeleteByID(ss.ctx, ss.sess, st.placeholder.ID); mErr != nil {
			slog.Warn("delete empty placeholder", "id", st.placeholder.ID, "error", mErr.Error())
		}
		ss.svc.broadcaster.EmitMessageDone(ss.ctx, ss.wsID, &realtime.MessageDonePayload{
			MessageId: st.placeholder.ID, ConversationId: ss.convID,
			AgentId: st.agent.ID, Status: string(orchestrator.MessageStatusSilent),
		})
		ss.svc.broadcaster.EmitAgentStatus(ss.ctx, ss.wsID, st.agent.ID, string(orchestrator.AgentStatusOnline), ss.convID)
		return nil
	}

	// Determine status.
	status := orchestrator.MessageStatusDelivered
	switch {
	case text == agentSilentResponse:
		status = orchestrator.MessageStatusSilent
		text = ""
	case !cleanDone:
		status = orchestrator.MessageStatusIncomplete
	}

	reply := ss.finalizeMessage(st.placeholder, text, status)
	ss.svc.broadcaster.EmitMessageDone(ss.ctx, ss.wsID, &realtime.MessageDonePayload{
		MessageId: st.placeholder.ID, ConversationId: ss.convID,
		AgentId: st.agent.ID, Status: string(status),
	})
	ss.svc.broadcaster.EmitAgentStatus(ss.ctx, ss.wsID, st.agent.ID, string(orchestrator.AgentStatusOnline), ss.convID)
	return reply
}

// finalizeMessage updates the placeholder message with final text and status,
// persists it, and broadcasts message.new.
func (ss *streamSession) finalizeMessage(placeholder *orchestrator.Message, text string, status orchestrator.MessageStatus) *orchestrator.Message {
	now := time.Now().UTC()
	placeholder.Content.Text = text
	placeholder.Status = status
	placeholder.UpdatedAt = now

	convCopy := *ss.conversation
	convCopy.UpdatedAt = now
	convCopy.LastMessage = placeholder

	if _, mErr := database.WithTransaction(ss.sess, "finalize stream message", func(tx *dbr.Tx) (*orchestrator.Message, *merrors.Error) {
		if mErr := ss.svc.messageRepo.Save(ss.ctx, tx, placeholder); mErr != nil {
			return nil, mErr
		}
		if mErr := ss.svc.conversationRepo.Save(ss.ctx, tx, &convCopy); mErr != nil {
			return nil, mErr
		}
		return placeholder, nil
	}); mErr != nil {
		slog.Warn("finalize stream message failed", "placeholder", placeholder.ID, "error", mErr.Error())
		return nil
	}

	if placeholder.AgentID != nil {
		placeholder.Agent = ss.lookups.byID[*placeholder.AgentID]
	}

	ss.svc.broadcaster.EmitMessageNew(ss.ctx, ss.wsID, placeholder)
	return placeholder
}
