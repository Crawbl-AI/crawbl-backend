package chatservice

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gocraft/dbr/v2"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// persistedMsg carries the mutable state produced by persistUserMessage.
// It lives on streamSession rather than on SendMessageOpts so that opts
// remains read-only after construction.
// NOTE: onPersisted is a deprecated escape hatch — see the TODO in
// persistUserMessage for the planned refactor to return userMsg directly.
type persistedMsg struct {
	userMessageID string
	localID       string
	deliveredOnce *sync.Once
	readOnce      *sync.Once
	onPersisted   func(*orchestrator.Message)
}

// SendMessage sends a user message and returns the agent replies.
// Dispatches to sendDirectMessage (per-agent conversations) or
// sendSwarmMessage (swarm group chat with parallel agent calls).
func (s *Service) SendMessage(ctx context.Context, opts *orchestratorservice.SendMessageOpts) ([]*orchestrator.Message, *merrors.Error) {
	if opts == nil {
		return nil, merrors.ErrInvalidInput
	}
	if opts.Content.Type != orchestrator.MessageContentTypeText || strings.TrimSpace(opts.Content.Text) == "" {
		return nil, merrors.ErrUnsupportedMessage
	}
	sess := database.SessionFromContext(ctx)

	workspace, agents, _, mErr := s.ensureWorkspaceBootstrap(ctx, sess, opts.UserID, opts.WorkspaceID)
	if mErr != nil {
		return nil, mErr
	}

	conversation, mErr := s.conversationRepo.GetByID(ctx, sess, opts.WorkspaceID, opts.ConversationID)
	if mErr != nil {
		return nil, mErr
	}

	runtimeState, mErr := s.runtimeClient.EnsureRuntime(ctx, &userswarmclient.EnsureRuntimeOpts{
		UserID:          workspace.UserID,
		WorkspaceID:     workspace.ID,
		WaitForVerified: true,
	})
	if mErr != nil {
		return nil, mErr
	}

	for _, agent := range agents {
		agent.Status = orchestrator.StatusForRuntime(runtimeState)
	}

	// Pre-flight quota check: reject if user exceeded monthly token limit.
	if s.usageRepo != nil {
		period := time.Now().UTC().Format("2006-01")
		tokensUsed, tokenLimit, qErr := s.usageRepo.CheckQuota(ctx, sess, opts.UserID, period)
		if qErr != nil {
			slog.Warn("quota check failed, allowing request", "user_id", opts.UserID, "error", qErr.Error())
		} else if tokenLimit > 0 && tokensUsed >= tokenLimit {
			return nil, merrors.NewBusinessError("monthly token quota exceeded", merrors.ErrCodeQuotaExceeded)
		}
	}

	if conversation.Type == orchestrator.ConversationTypeSwarm {
		return s.sendSwarmMessage(ctx, opts, conversation, agents, runtimeState)
	}
	return s.sendDirectMessage(ctx, opts, conversation, agents, runtimeState)
}

// sendDirectMessage handles per-agent conversations: persist the user message,
// then stream the agent response via callAgentStreaming.
func (s *Service) sendDirectMessage(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	conversation *orchestrator.Conversation,
	agents []*orchestrator.Agent,
	runtimeState *orchestrator.RuntimeStatus,
) ([]*orchestrator.Message, *merrors.Error) {
	responders := resolveResponders(conversation, agents, opts.Mentions)
	lookups := newAgentLookups(agents)

	var primaryResponder *orchestrator.Agent
	if len(responders) > 0 {
		primaryResponder = responders[0]
	}
	if primaryResponder == nil {
		return nil, merrors.ErrAgentNotFound
	}

	// Persist user message first (same as swarm path).
	pm, mErr := s.persistUserMessage(ctx, opts, conversation)
	if mErr != nil {
		return nil, mErr
	}

	// Stream response from the agent.
	return s.callAgentStreaming(ctx, callAgentStreamingOpts{
		sendOpts:     opts,
		pm:           pm,
		conversation: conversation,
		runtimeState: runtimeState,
		agent:        primaryResponder,
		lookups:      lookups,
		extraContext: "",
	})
}

// sendSwarmMessage handles swarm group chat: persist user message first,
// resolve target agents via mentions or Manager, then execute.
func (s *Service) sendSwarmMessage(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	conversation *orchestrator.Conversation,
	agents []*orchestrator.Agent,
	runtimeState *orchestrator.RuntimeStatus,
) ([]*orchestrator.Message, *merrors.Error) {
	// 1. Persist user message first so it's visible immediately.
	pm, mErr := s.persistUserMessage(ctx, opts, conversation)
	if mErr != nil {
		return nil, mErr
	}

	lookups := newAgentLookups(agents)

	// 2. Resolve target agents: mentions first, then Manager.
	responders := resolveResponders(conversation, agents, opts.Mentions)

	if responders != nil {
		// Mentions resolved — send directly to mentioned agents (parallel).
		return s.executeParallel(ctx, opts, pm, conversation, runtimeState, responders, lookups)
	}

	// 3. No mentions — send to Manager. Manager has full context (memory,
	// conversation history, SOUL.md, tools including delegate) and decides
	// autonomously whether to answer directly or delegate to sub-agents.
	if lookups.manager == nil {
		return nil, merrors.ErrAgentNotFound
	}

	// Build conversation context so Manager sees recent chat history.
	sess := database.SessionFromContext(ctx)
	conversationContext := s.buildConversationContext(ctx, sess, opts.WorkspaceID, conversation.ID, lookups, 20)

	return s.callAgentStreaming(ctx, callAgentStreamingOpts{
		sendOpts:     opts,
		pm:           pm,
		conversation: conversation,
		runtimeState: runtimeState,
		agent:        lookups.manager,
		lookups:      lookups,
		extraContext: conversationContext,
	})
}

// executeParallel fires all agent calls concurrently. Each agent responds
// independently without seeing other agents' current responses.
// Each goroutine owns its own dbr.Session so concurrent repo calls are safe.
func (s *Service) executeParallel(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	pm *persistedMsg,
	conversation *orchestrator.Conversation,
	runtimeState *orchestrator.RuntimeStatus,
	targetAgents []*orchestrator.Agent,
	lookups agentLookups,
) ([]*orchestrator.Message, *merrors.Error) {
	results := make([]agentResult, len(targetAgents))
	var wg sync.WaitGroup
	wg.Add(len(targetAgents))

	for i, agent := range targetAgents {
		go func(idx int, ag *orchestrator.Agent) {
			defer wg.Done()
			// Create a per-goroutine session — dbr.Session is not goroutine-safe.
			// The session travels on a derived context so opts stays read-only.
			agentSess := s.db.NewSession(nil)
			agentCtx := database.ContextWithSession(ctx, agentSess)
			replies, err := s.callAgentStreaming(agentCtx, callAgentStreamingOpts{
				sendOpts:     opts,
				pm:           pm,
				conversation: conversation,
				runtimeState: runtimeState,
				agent:        ag,
				lookups:      lookups,
				extraContext: "",
			})
			results[idx] = agentResult{replies: replies, err: err}
		}(i, agent)
	}

	wg.Wait()

	var replies []*orchestrator.Message
	var lastErr *merrors.Error
	for _, r := range results {
		if len(r.replies) > 0 {
			replies = append(replies, r.replies...)
		}
		if r.err != nil {
			lastErr = r.err
		}
	}

	if len(replies) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, merrors.NewServerErrorText(fmt.Sprintf("no agent produced a response for conversation %s", conversation.ID))
	}
	return replies, nil
}

// persistUserMessage saves the user message in its own transaction,
// broadcasts message.new, and returns the mutable tracking state as a
// persistedMsg. SendMessageOpts is not mutated — it remains read-only
// after construction.
func (s *Service) persistUserMessage(
	ctx context.Context,
	opts *orchestratorservice.SendMessageOpts,
	conversation *orchestrator.Conversation,
) (*persistedMsg, *merrors.Error) {
	userMsg := newMessage(
		conversation.ID,
		orchestrator.MessageRoleUser,
		opts.Content,
		orchestrator.MessageStatusSent,
		nil,
		append([]orchestrator.Attachment(nil), opts.Attachments...),
	)
	userMsg.LocalID = stringPtr(opts.LocalID)

	sess := database.SessionFromContext(ctx)
	if _, mErr := database.WithTransaction(sess, "persist user message", func(tx *dbr.Tx) (*orchestrator.Message, *merrors.Error) {
		if mErr := s.messageRepo.Save(ctx, tx, userMsg); mErr != nil {
			return nil, mErr
		}
		return userMsg, nil
	}); mErr != nil {
		return nil, mErr
	}

	s.broadcaster.EmitMessageNew(ctx, opts.WorkspaceID, userMsg)

	// TODO(abbasaghababayev): remove OnPersisted callback — SendMessage should
	// return the persisted userMsg directly so the handler can ack without a
	// closure that mutates transport state from the service layer (RULE-13).
	if opts.OnPersisted != nil {
		opts.OnPersisted(userMsg)
	}

	return &persistedMsg{
		userMessageID: userMsg.ID,
		localID:       opts.LocalID,
		deliveredOnce: &sync.Once{},
		readOnce:      &sync.Once{},
		onPersisted:   opts.OnPersisted,
	}, nil
}
