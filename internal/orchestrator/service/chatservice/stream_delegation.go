package chatservice

import (
	"context"
	"log/slog"
	"time"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
)

// createSubAgentStream creates a new placeholder and stream entry for a sub-agent.
func (ss *streamSession) createSubAgentStream(sub *orchestrator.Agent) *subAgentStream {
	placeholder := ss.svc.newPlaceholder(ss.convID, sub)
	if mErr := ss.svc.savePlaceholder(ss.ctx, ss.sess, placeholder); mErr != nil {
		slog.Warn("sub-agent placeholder failed, routing to primary", "sub", sub.Slug, "error", mErr.Error())
		return ss.streams[ss.primary.ID]
	}

	ss.svc.broadcaster.EmitAgentStatus(ss.ctx, ss.wsID, sub.ID, string(orchestrator.AgentStatusThinking), ss.convID)

	// Emit delegation running. ADK may not surface transfer_to_agent as a
	// ToolCallEvent, so this is the reliable place to emit delegation --
	// when the first chunk from the sub-agent creates the stream.
	delegationCreatedAt := ""
	delegationMsgID := ""
	if primarySt, ok := ss.streams[ss.primary.ID]; ok {
		delegationCreatedAt = primarySt.placeholder.CreatedAt.UTC().Format(time.RFC3339Nano)
		delegationMsgID = primarySt.placeholder.ID
	}
	ss.svc.broadcaster.EmitAgentDelegation(ss.ctx, ss.wsID, &realtime.AgentDelegationPayload{
		From:           delegationAgent(ss.primary),
		To:             delegationAgent(sub),
		ConversationId: ss.convID, Status: realtime.AgentDelegationStatusRunning,
		MessageId: delegationMsgID,
		CreatedAt: delegationCreatedAt,
	})
	// Manager handed off -- clear thinking status so mobile stops showing
	// "Manager is thinking" while the sub-agent works.
	ss.svc.broadcaster.EmitAgentStatus(ss.ctx, ss.wsID, ss.primary.ID, string(orchestrator.AgentStatusOnline), ss.convID)

	st := &subAgentStream{agent: sub, placeholder: placeholder, firstChunk: true}
	ss.streams[sub.ID] = st
	return st
}

func (s *Service) updateDelegationSummary(triggerMsgID, summary string) {
	if summary == "" {
		return
	}
	auditCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess := s.db.NewSession(nil)
	if mErr := s.messageRepo.UpdateDelegationSummary(auditCtx, sess, triggerMsgID, summary); mErr != nil {
		slog.Warn("updateDelegationSummary: failed to backfill task_summary",
			"trigger_message_id", triggerMsgID,
			"error", mErr.Error(),
		)
	}
}

func (s *Service) completeDelegation(_, _, triggerMsgID, delegateAgentID string) {
	auditCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess := s.db.NewSession(nil)
	if mErr := s.messageRepo.CompleteDelegation(auditCtx, sess, triggerMsgID, delegateAgentID); mErr != nil {
		slog.Warn("completeDelegation: failed to mark delegation completed",
			"trigger_message_id", triggerMsgID,
			"delegate", delegateAgentID,
			"error", mErr.Error(),
		)
	}
}
