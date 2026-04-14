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

	if reply := ss.finalizePrimaryDelegation(); reply != nil {
		replies = append(replies, reply)
	}

	for _, st := range ss.streams {
		if ss.isDelegatingPrimary(st) {
			continue
		}
		if reply := ss.finalizeSubAgent(st); reply != nil {
			replies = append(replies, reply)
		}
	}

	ss.emitSubAgentDelegationSweep()
	return replies
}

// finalizePrimaryDelegation finalizes the primary agent's message when
// delegation occurred (i.e. more than one stream exists for this turn).
// Returns nil when no delegation happened.
func (ss *streamSession) finalizePrimaryDelegation() *orchestrator.Message {
	primarySt := ss.streams[ss.primary.ID]
	if primarySt == nil || len(ss.streams) <= 1 {
		return nil
	}
	text := strings.TrimSpace(primarySt.accumulated.String())
	delegatee := ss.pickDelegatee()

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

// pickDelegatee returns a deterministic sub-agent for multi-delegation turns
// by picking the lexicographically smallest non-primary stream ID.
func (ss *streamSession) pickDelegatee() *orchestrator.Agent {
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

// isDelegatingPrimary reports whether st is the primary stream in a
// delegation turn — those are finalized earlier, so the sub-agent loop
// must skip them.
func (ss *streamSession) isDelegatingPrimary(st *subAgentStream) bool {
	return st.agent.ID == ss.primary.ID && len(ss.streams) > 1
}

// finalizeSubAgent persists a single sub-agent stream. Empty streams have
// their placeholder deleted; the rest are saved with the derived status.
func (ss *streamSession) finalizeSubAgent(st *subAgentStream) *orchestrator.Message {
	text := strings.TrimSpace(st.accumulated.String())
	if text == "" {
		ss.discardEmptyStream(st)
		return nil
	}

	status, finalText := subAgentStatus(text, st.done || ss.globalDone)
	reply := ss.finalizeMessage(st.placeholder, finalText, status)
	ss.svc.broadcaster.EmitMessageDone(ss.ctx, ss.wsID, realtime.MessageDonePayload{
		MessageID: st.placeholder.ID, ConversationID: ss.convID,
		AgentID: st.agent.ID, Status: string(status),
	})
	ss.svc.broadcaster.EmitAgentStatus(ss.ctx, ss.wsID, st.agent.ID, string(orchestrator.AgentStatusOnline), ss.convID)
	return reply
}

// discardEmptyStream deletes the placeholder and emits a silent done event.
func (ss *streamSession) discardEmptyStream(st *subAgentStream) {
	if mErr := ss.svc.messageRepo.DeleteByID(ss.ctx, ss.sess, st.placeholder.ID); mErr != nil {
		slog.Warn("delete empty placeholder", "id", st.placeholder.ID, "error", mErr.Error())
	}
	ss.svc.broadcaster.EmitMessageDone(ss.ctx, ss.wsID, realtime.MessageDonePayload{
		MessageID: st.placeholder.ID, ConversationID: ss.convID,
		AgentID: st.agent.ID, Status: string(orchestrator.MessageStatusSilent),
	})
	ss.svc.broadcaster.EmitAgentStatus(ss.ctx, ss.wsID, st.agent.ID, string(orchestrator.AgentStatusOnline), ss.convID)
}

// subAgentStatus maps the accumulated text + done flag onto the final
// (status, text) pair. The sentinel [SILENT] collapses to an empty text.
func subAgentStatus(text string, cleanDone bool) (orchestrator.MessageStatus, string) {
	switch {
	case text == agentSilentResponse:
		return orchestrator.MessageStatusSilent, ""
	case !cleanDone:
		return orchestrator.MessageStatusIncomplete, text
	default:
		return orchestrator.MessageStatusDelivered, text
	}
}

// emitSubAgentDelegationSweep emits a completion event for every sub-agent
// stream so late-arriving UI never stays stuck in a "running" state.
func (ss *streamSession) emitSubAgentDelegationSweep() {
	for _, st := range ss.streams {
		if st.agent.ID == ss.primary.ID {
			continue
		}
		ss.svc.broadcaster.EmitAgentDelegation(ss.ctx, ss.wsID, realtime.AgentDelegationPayload{
			From:           delegationAgent(ss.primary),
			To:             delegationAgent(st.agent),
			ConversationID: ss.convID,
			Status:         realtime.AgentDelegationStatusCompleted,
			MessageID:      st.placeholder.ID,
		})
	}
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
