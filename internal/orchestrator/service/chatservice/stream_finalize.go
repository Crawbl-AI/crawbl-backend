package chatservice

import (
	"log/slog"
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
		text := strings.TrimSpace(primarySt.accumulated.String())

		// Find a sub-agent that was delegated to. In multi-delegation scenarios
		// (rare), the picked agent is non-deterministic -- acceptable because
		// the delegation card is a summary, not per-sub-agent.
		var delegatee *orchestrator.Agent
		for _, st := range ss.streams {
			if st.agent.ID != ss.primary.ID {
				delegatee = st.agent
				break
			}
		}

		primarySt.placeholder.Content = orchestrator.MessageContent{
			Type:        orchestrator.MessageContentTypeDelegation,
			Text:        text,
			From:        orchestrator.ContentAgentFromAgent(ss.primary),
			To:          orchestrator.ContentAgentFromAgent(delegatee),
			Status:      realtime.AgentDelegationStatusCompleted,
			TaskPreview: truncateText(text, taskPreviewMaxRunes),
		}
		if reply := ss.finalizeMessage(primarySt.placeholder, text, orchestrator.MessageStatusDelegated); reply != nil {
			replies = append(replies, reply)
		}
		ss.svc.updateDelegationSummary(ss.placeholder.ID, text)
		ss.svc.broadcaster.EmitAgentStatus(ss.ctx, ss.wsID, ss.primary.ID, string(orchestrator.AgentStatusOnline), ss.convID)
	}

	// Remaining streams.
	for _, st := range ss.streams {
		if st.agent.ID == ss.primary.ID && len(ss.streams) > 1 {
			continue
		}
		text := strings.TrimSpace(st.accumulated.String())
		cleanDone := st.done || ss.globalDone

		// Empty -- delete placeholder.
		if text == "" {
			if mErr := ss.svc.messageRepo.DeleteByID(ss.ctx, ss.sess, st.placeholder.ID); mErr != nil {
				slog.Warn("delete empty placeholder", "id", st.placeholder.ID, "error", mErr.Error())
			}
			ss.svc.broadcaster.EmitMessageDone(ss.ctx, ss.wsID, realtime.MessageDonePayload{
				MessageID: st.placeholder.ID, ConversationID: ss.convID,
				AgentID: st.agent.ID, Status: string(orchestrator.MessageStatusSilent),
			})
			ss.svc.broadcaster.EmitAgentStatus(ss.ctx, ss.wsID, st.agent.ID, string(orchestrator.AgentStatusOnline), ss.convID)
			continue
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

		if reply := ss.finalizeMessage(st.placeholder, text, status); reply != nil {
			replies = append(replies, reply)
		}
		ss.svc.broadcaster.EmitMessageDone(ss.ctx, ss.wsID, realtime.MessageDonePayload{
			MessageID: st.placeholder.ID, ConversationID: ss.convID,
			AgentID: st.agent.ID, Status: string(status),
		})
		ss.svc.broadcaster.EmitAgentStatus(ss.ctx, ss.wsID, st.agent.ID, string(orchestrator.AgentStatusOnline), ss.convID)
	}

	// Safety sweep: emit delegation completion for all sub-agents.
	for _, st := range ss.streams {
		if st.agent.ID != ss.primary.ID {
			ss.svc.broadcaster.EmitAgentDelegation(ss.ctx, ss.wsID, realtime.AgentDelegationPayload{
				From:           delegationAgent(ss.primary),
				To:             delegationAgent(st.agent),
				ConversationID: ss.convID, Status: realtime.AgentDelegationStatusCompleted,
				MessageID: st.placeholder.ID,
			})
		}
	}

	return replies
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
