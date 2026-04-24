package chatservice

import (
	"context"
	"testing"
	"time"

	"github.com/gocraft/dbr/v2"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/messagerepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/usagerepo"
	orchestratorservice "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/service"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
	userswarmclient "github.com/Crawbl-AI/crawbl-backend/internal/userswarm/client"
)

// actionsWorkspaceStore is a workspace store fake for actions tests.
type actionsWorkspaceStore struct {
	ws  *orchestrator.Workspace
	err *merrors.Error
}

func (f *actionsWorkspaceStore) GetByID(_ context.Context, _ orchestratorrepo.SessionRunner, _, _ string) (*orchestrator.Workspace, *merrors.Error) {
	return f.ws, f.err
}

// actionsAgentStore is a minimal agent store fake for actions tests.
type actionsAgentStore struct{}

func (actionsAgentStore) ListByWorkspaceID(_ context.Context, _ orchestratorrepo.SessionRunner, _ string) ([]*orchestrator.Agent, *merrors.Error) {
	return nil, nil
}

func (actionsAgentStore) GetByIDGlobal(_ context.Context, _ orchestratorrepo.SessionRunner, _ string) (*orchestrator.Agent, *merrors.Error) {
	return nil, merrors.ErrAgentNotFound
}

func (actionsAgentStore) Save(_ context.Context, _ orchestratorrepo.SessionRunner, _ *orchestrator.Agent, _ int) *merrors.Error {
	return nil
}

// actionsConversationStore is a conversation store fake for actions tests.
type actionsConversationStore struct {
	conv *orchestrator.Conversation
	err  *merrors.Error
}

func (f *actionsConversationStore) ListByWorkspaceID(_ context.Context, _ orchestratorrepo.SessionRunner, _ string) ([]*orchestrator.Conversation, *merrors.Error) {
	return nil, nil
}

func (f *actionsConversationStore) GetByID(_ context.Context, _ orchestratorrepo.SessionRunner, _, _ string) (*orchestrator.Conversation, *merrors.Error) {
	return f.conv, f.err
}

func (f *actionsConversationStore) FindDefaultSwarm(_ context.Context, _ orchestratorrepo.SessionRunner, _ string) (*orchestrator.Conversation, *merrors.Error) {
	return nil, nil
}

func (f *actionsConversationStore) Save(_ context.Context, _ orchestratorrepo.SessionRunner, _ *orchestrator.Conversation) *merrors.Error {
	return nil
}

func (f *actionsConversationStore) Create(_ context.Context, _ orchestratorrepo.SessionRunner, _ *orchestrator.Conversation) *merrors.Error {
	return nil
}

func (f *actionsConversationStore) Delete(_ context.Context, _ orchestratorrepo.SessionRunner, _, _ string) *merrors.Error {
	return nil
}

func (f *actionsConversationStore) MarkAsRead(_ context.Context, _ orchestratorrepo.SessionRunner, _, _ string) *merrors.Error {
	return nil
}

// actionsMessageStore records saves and controls GetByID response.
type actionsMessageStore struct {
	msgByID map[string]*orchestrator.Message
	saved   []*orchestrator.Message
	saveErr *merrors.Error
	getErr  *merrors.Error
}

func newActionsMessageStore() *actionsMessageStore {
	return &actionsMessageStore{msgByID: make(map[string]*orchestrator.Message)}
}

func (f *actionsMessageStore) register(msg *orchestrator.Message) {
	f.msgByID[msg.ID] = msg
}

func (f *actionsMessageStore) ListByConversationID(_ context.Context, _ orchestratorrepo.SessionRunner, _ *orchestratorrepo.ListMessagesOpts) (*orchestrator.MessagePage, *merrors.Error) {
	return &orchestrator.MessagePage{}, nil
}

func (f *actionsMessageStore) GetLatestByConversationID(_ context.Context, _ orchestratorrepo.SessionRunner, _ string) (*orchestrator.Message, *merrors.Error) {
	return nil, nil
}

func (f *actionsMessageStore) GetLatestByConversationIDs(_ context.Context, _ orchestratorrepo.SessionRunner, _ []string) (map[string]*orchestrator.Message, *merrors.Error) {
	return nil, nil
}

func (f *actionsMessageStore) GetByID(_ context.Context, _ orchestratorrepo.SessionRunner, messageID string) (*orchestrator.Message, *merrors.Error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if msg, ok := f.msgByID[messageID]; ok {
		return msg, nil
	}
	return nil, merrors.ErrMessageNotFound
}

func (f *actionsMessageStore) Save(_ context.Context, _ orchestratorrepo.SessionRunner, msg *orchestrator.Message) *merrors.Error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, msg)
	return nil
}

func (f *actionsMessageStore) UpdateStatus(_ context.Context, _ orchestratorrepo.SessionRunner, _ string, _ orchestrator.MessageStatus) *merrors.Error {
	return nil
}

func (f *actionsMessageStore) DeleteByID(_ context.Context, _ orchestratorrepo.SessionRunner, _ string) *merrors.Error {
	return nil
}

func (f *actionsMessageStore) ListRecent(_ context.Context, _ orchestratorrepo.SessionRunner, _ string, _ int) ([]*orchestrator.Message, *merrors.Error) {
	return nil, nil
}

func (f *actionsMessageStore) RecordDelegation(_ context.Context, _ orchestratorrepo.SessionRunner, _ messagerepo.RecordDelegationOpts) *merrors.Error {
	return nil
}

func (f *actionsMessageStore) CompleteDelegation(_ context.Context, _ orchestratorrepo.SessionRunner, _, _ string) *merrors.Error {
	return nil
}

func (f *actionsMessageStore) UpdateDelegationSummary(_ context.Context, _ orchestratorrepo.SessionRunner, _, _ string) *merrors.Error {
	return nil
}

func (f *actionsMessageStore) UpdateToolState(_ context.Context, _ orchestratorrepo.SessionRunner, _, _ string) *merrors.Error {
	return nil
}

// spyActionsBroadcaster records EmitMessageUpdated calls.
type spyActionsBroadcaster struct {
	realtime.NopBroadcaster
	updatedCalls int
	lastUpdated  any
	newCalls     int
}

func (s *spyActionsBroadcaster) EmitMessageUpdated(_ context.Context, _ string, data any) {
	s.updatedCalls++
	s.lastUpdated = data
}

func (s *spyActionsBroadcaster) EmitMessageNew(_ context.Context, _ string, _ any) {
	s.newCalls++
}

// successSendFn returns a sendMessageFunc that always succeeds, capturing
// the opts passed to it for optional assertion.
func successSendFn(captured *[]*orchestratorservice.SendMessageOpts) sendMessageFunc {
	return func(_ context.Context, opts *orchestratorservice.SendMessageOpts) ([]*orchestrator.Message, *merrors.Error) {
		if captured != nil {
			*captured = append(*captured, opts)
		}
		return []*orchestrator.Message{{ID: "follow-" + opts.LocalID}}, nil
	}
}

// failingSendFn returns a sendMessageFunc that always fails with a typed error.
func failingSendFn() sendMessageFunc {
	return func(_ context.Context, _ *orchestratorservice.SendMessageOpts) ([]*orchestrator.Message, *merrors.Error) {
		return nil, merrors.NewBusinessError("simulated send failure", merrors.ErrCodeBadRequest)
	}
}

// buildActionsService constructs a Service for actions testing.
// The dbr.Connection is a zero-value placeholder; it is only used in SendMessage's
// streaming path which is not exercised in these unit tests.
func buildActionsService(
	ws *actionsWorkspaceStore,
	conv *actionsConversationStore,
	msgs *actionsMessageStore,
	broadcaster realtime.Broadcaster,
) *Service {
	return buildActionsServiceWithSendFn(ws, conv, msgs, broadcaster, successSendFn(nil))
}

// buildActionsServiceWithSendFn constructs a Service with a custom follow-up
// sender injected. Lets tests simulate send success/failure without the full
// streaming pipeline.
func buildActionsServiceWithSendFn(
	ws *actionsWorkspaceStore,
	conv *actionsConversationStore,
	msgs *actionsMessageStore,
	broadcaster realtime.Broadcaster,
	sendFn sendMessageFunc,
) *Service {
	return &Service{
		db:                &dbr.Connection{},
		workspaceRepo:     ws,
		agentRepo:         actionsAgentStore{},
		conversationRepo:  conv,
		messageRepo:       msgs,
		toolsRepo:         noopToolsStore{},
		agentSettingsRepo: noopAgentSettingsStore{},
		agentPromptsRepo:  noopAgentPromptsStore{},
		agentHistoryRepo:  noopAgentHistoryStore{},
		usageRepo:         noopUsageRepo{},
		runtimeClient:     userswarmclient.NewFakeClient(userswarmclient.Config{}),
		broadcaster:       broadcaster,
		defaultAgents:     orchestrator.GetDefaultAgents(),
		sendMessageFn:     sendFn,
	}
}

const testQuestionsConvID = "conv-1"

// buildQuestionsMessage builds a Message of type questions for test use.
func buildQuestionsMessage(id, workspaceID string) *orchestrator.Message {
	_ = workspaceID // ownership checked via conversation, not stored on message
	return &orchestrator.Message{
		ID:             id,
		ConversationID: testQuestionsConvID,
		Role:           orchestrator.MessageRoleAgent,
		Status:         orchestrator.MessageStatusDelivered,
		Content: orchestrator.MessageContent{
			Type: orchestrator.MessageContentTypeQuestions,
			Turns: []orchestrator.QuestionTurn{
				{
					Index: 1,
					Questions: []orchestrator.QuestionItem{
						{
							ID:     "t1q1",
							Prompt: "Favourite city?",
							Mode:   orchestrator.QuestionModeSingle,
							Options: []orchestrator.QuestionOption{
								{ID: "A", Label: "Paris"},
								{ID: "B", Label: "London"},
								{ID: "C", Label: "Tokyo"},
							},
							AllowCustom: true,
						},
						{
							ID:     "t1q2",
							Prompt: "Hobbies?",
							Mode:   orchestrator.QuestionModeMulti,
							Options: []orchestrator.QuestionOption{
								{ID: "A", Label: "Reading"},
								{ID: "B", Label: "Gaming"},
								{ID: "C", Label: "Cooking"},
							},
							AllowCustom: false,
						},
					},
				},
			},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
}

// ctxWithNilSession returns a context carrying a nil *dbr.Session so that
// SessionFromContext returns nil without panicking. Fakes accept nil sessions.
func ctxWithNilSession() context.Context {
	return database.ContextWithSession(context.Background(), nil)
}

func TestRespondToQuestions_HappyPath(t *testing.T) {
	t.Parallel()

	spy := &spyActionsBroadcaster{}
	msgs := newActionsMessageStore()
	ws := &actionsWorkspaceStore{ws: &orchestrator.Workspace{ID: "ws-1", UserID: "user-1"}}
	conv := &actionsConversationStore{conv: &orchestrator.Conversation{ID: "conv-1", WorkspaceID: "ws-1"}}

	qMsg := buildQuestionsMessage("msg-1", "ws-1")
	msgs.register(qMsg)

	svc := buildActionsService(ws, conv, msgs, spy)

	opts := &orchestratorservice.RespondToQuestionsOpts{
		UserID:      "user-1",
		WorkspaceID: "ws-1",
		MessageID:   "msg-1",
		Answers: []orchestratorservice.QuestionAnswerInput{
			{QuestionID: "t1q1", OptionIDs: []string{"A"}},
			{QuestionID: "t1q2", OptionIDs: []string{"B", "C"}},
		},
	}

	result, mErr := svc.RespondToQuestions(ctxWithNilSession(), opts)
	if mErr != nil {
		t.Fatalf("RespondToQuestions returned unexpected error: %v", mErr)
	}

	if result == nil {
		t.Fatal("expected non-nil result message")
	}
	if result.ID != "msg-1" {
		t.Fatalf("result.ID = %q, want %q", result.ID, "msg-1")
	}

	if len(result.Content.Answers) != 2 {
		t.Fatalf("answers count = %d, want 2", len(result.Content.Answers))
	}
	if result.Content.Answers[0].QuestionID != "t1q1" {
		t.Fatalf("answers[0].QuestionID = %q, want %q", result.Content.Answers[0].QuestionID, "t1q1")
	}
	if len(result.Content.Answers[0].OptionIDs) != 1 || result.Content.Answers[0].OptionIDs[0] != "A" {
		t.Fatalf("answers[0].OptionIDs = %v, want [A]", result.Content.Answers[0].OptionIDs)
	}
	if result.Content.Answers[1].QuestionID != "t1q2" {
		t.Fatalf("answers[1].QuestionID = %q, want %q", result.Content.Answers[1].QuestionID, "t1q2")
	}

	if spy.updatedCalls != 1 {
		t.Fatalf("EmitMessageUpdated called %d times, want 1", spy.updatedCalls)
	}

	// Verify the saved message carries the answers.
	if len(msgs.saved) < 1 {
		t.Fatal("expected at least 1 saved message")
	}
	saved := msgs.saved[0]
	if len(saved.Content.Answers) != 2 {
		t.Fatalf("saved answers count = %d, want 2", len(saved.Content.Answers))
	}
}

func TestRespondToQuestions_ValidationErrors(t *testing.T) {
	t.Parallel()

	ws := &actionsWorkspaceStore{ws: &orchestrator.Workspace{ID: "ws-1", UserID: "user-1"}}
	conv := &actionsConversationStore{conv: &orchestrator.Conversation{ID: "conv-1", WorkspaceID: "ws-1"}}

	tests := []struct {
		name    string
		msgFn   func() *orchestrator.Message
		answers []orchestratorservice.QuestionAnswerInput
		wantErr *merrors.Error
	}{
		{
			name: "message type is text not questions",
			msgFn: func() *orchestrator.Message {
				msg := buildQuestionsMessage("msg-text", "ws-1")
				msg.Content.Type = orchestrator.MessageContentTypeText
				return msg
			},
			answers: []orchestratorservice.QuestionAnswerInput{
				{QuestionID: "t1q1", OptionIDs: []string{"A"}},
			},
			wantErr: merrors.ErrUnsupportedMessage,
		},
		{
			name:  "unknown question_id in answers",
			msgFn: func() *orchestrator.Message { return buildQuestionsMessage("msg-unk-q", "ws-1") },
			answers: []orchestratorservice.QuestionAnswerInput{
				{QuestionID: "unknown-q", OptionIDs: []string{"A"}},
			},
		},
		{
			name:  "unknown option_id for valid question",
			msgFn: func() *orchestrator.Message { return buildQuestionsMessage("msg-unk-opt", "ws-1") },
			answers: []orchestratorservice.QuestionAnswerInput{
				{QuestionID: "t1q1", OptionIDs: []string{"Z"}},
			},
		},
		{
			name:  "single-mode question with two option IDs",
			msgFn: func() *orchestrator.Message { return buildQuestionsMessage("msg-multi-single", "ws-1") },
			answers: []orchestratorservice.QuestionAnswerInput{
				{QuestionID: "t1q1", OptionIDs: []string{"A", "B"}},
			},
		},
		{
			name:  "custom text on question with AllowCustom false",
			msgFn: func() *orchestrator.Message { return buildQuestionsMessage("msg-no-custom", "ws-1") },
			answers: []orchestratorservice.QuestionAnswerInput{
				// t1q2 has AllowCustom=false.
				{QuestionID: "t1q2", OptionIDs: []string{"A"}, CustomText: "My custom answer"},
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			msgs := newActionsMessageStore()
			msg := tc.msgFn()
			msgs.register(msg)
			svc := buildActionsService(ws, conv, msgs, realtime.NopBroadcaster{})

			opts := &orchestratorservice.RespondToQuestionsOpts{
				UserID:      "user-1",
				WorkspaceID: "ws-1",
				MessageID:   msg.ID,
				Answers:     tc.answers,
			}

			_, mErr := svc.RespondToQuestions(ctxWithNilSession(), opts)
			if mErr == nil {
				t.Fatal("expected an error, got nil")
			}
			if tc.wantErr != nil && mErr.Code != tc.wantErr.Code {
				t.Fatalf("error code = %q, want %q", mErr.Code, tc.wantErr.Code)
			}
		})
	}
}

func TestRespondToQuestions_Idempotent(t *testing.T) {
	t.Parallel()

	msgs := newActionsMessageStore()
	qMsg := buildQuestionsMessage("msg-dup", "ws-1")
	// Pre-existing answers on the card simulate an already-answered state.
	qMsg.Content.Answers = []orchestrator.QuestionAnswer{
		{QuestionID: "t1q1", OptionIDs: []string{"A"}},
	}
	msgs.register(qMsg)

	ws := &actionsWorkspaceStore{ws: &orchestrator.Workspace{ID: "ws-1", UserID: "user-1"}}
	conv := &actionsConversationStore{conv: &orchestrator.Conversation{ID: "conv-1", WorkspaceID: "ws-1"}}
	svc := buildActionsService(ws, conv, msgs, realtime.NopBroadcaster{})

	opts := &orchestratorservice.RespondToQuestionsOpts{
		UserID:      "user-1",
		WorkspaceID: "ws-1",
		MessageID:   "msg-dup",
		Answers: []orchestratorservice.QuestionAnswerInput{
			{QuestionID: "t1q1", OptionIDs: []string{"B"}},
		},
	}

	_, mErr := svc.RespondToQuestions(ctxWithNilSession(), opts)
	if mErr == nil {
		t.Fatal("expected already-answered error, got nil")
	}
	if mErr.Code != merrors.ErrCodeQuestionAlreadyAnswered {
		t.Fatalf("error code = %q, want %q", mErr.Code, merrors.ErrCodeQuestionAlreadyAnswered)
	}
}

func TestRespondToQuestions_ZeroSelection(t *testing.T) {
	t.Parallel()

	msgs := newActionsMessageStore()
	msgs.register(buildQuestionsMessage("msg-zero", "ws-1"))

	ws := &actionsWorkspaceStore{ws: &orchestrator.Workspace{ID: "ws-1", UserID: "user-1"}}
	conv := &actionsConversationStore{conv: &orchestrator.Conversation{ID: "conv-1", WorkspaceID: "ws-1"}}
	svc := buildActionsService(ws, conv, msgs, realtime.NopBroadcaster{})

	opts := &orchestratorservice.RespondToQuestionsOpts{
		UserID:      "user-1",
		WorkspaceID: "ws-1",
		MessageID:   "msg-zero",
		Answers: []orchestratorservice.QuestionAnswerInput{
			// Neither option IDs nor custom text — must be rejected.
			{QuestionID: "t1q1"},
		},
	}

	_, mErr := svc.RespondToQuestions(ctxWithNilSession(), opts)
	if mErr == nil {
		t.Fatal("expected zero-selection validation error, got nil")
	}
}

func TestRespondToQuestions_FollowupSendFails(t *testing.T) {
	t.Parallel()

	msgs := newActionsMessageStore()
	msgs.register(buildQuestionsMessage("msg-follow-fail", "ws-1"))

	ws := &actionsWorkspaceStore{ws: &orchestrator.Workspace{ID: "ws-1", UserID: "user-1"}}
	conv := &actionsConversationStore{conv: &orchestrator.Conversation{ID: "conv-1", WorkspaceID: "ws-1"}}
	svc := buildActionsServiceWithSendFn(ws, conv, msgs, realtime.NopBroadcaster{}, failingSendFn())

	opts := &orchestratorservice.RespondToQuestionsOpts{
		UserID:      "user-1",
		WorkspaceID: "ws-1",
		MessageID:   "msg-follow-fail",
		Answers: []orchestratorservice.QuestionAnswerInput{
			{QuestionID: "t1q1", OptionIDs: []string{"A"}},
		},
	}

	result, mErr := svc.RespondToQuestions(ctxWithNilSession(), opts)
	if mErr == nil {
		t.Fatal("expected follow-up failure error, got nil")
	}
	if mErr.Code != merrors.ErrCodeQuestionFollowupFailed {
		t.Fatalf("error code = %q, want %q", mErr.Code, merrors.ErrCodeQuestionFollowupFailed)
	}
	// Answers must still be recorded despite the follow-up failure.
	if result == nil {
		t.Fatal("expected the updated message to be returned even on follow-up failure")
	}
	if len(result.Content.Answers) != 1 {
		t.Fatalf("answers count = %d, want 1", len(result.Content.Answers))
	}
	if len(msgs.saved) < 1 {
		t.Fatal("expected the message to have been saved")
	}
}

func TestRespondToQuestions_WorkspaceMismatch(t *testing.T) {
	t.Parallel()

	msgs := newActionsMessageStore()
	qMsg := buildQuestionsMessage("msg-1", "ws-other")
	msgs.register(qMsg)

	// Workspace store succeeds for ws-1, but the conversation belongs to ws-other.
	ws := &actionsWorkspaceStore{ws: &orchestrator.Workspace{ID: "ws-1", UserID: "user-1"}}
	// Conversation store returns not-found for ws-1 (ownership mismatch).
	conv := &actionsConversationStore{err: merrors.ErrConversationNotFound}
	svc := buildActionsService(ws, conv, msgs, realtime.NopBroadcaster{})

	opts := &orchestratorservice.RespondToQuestionsOpts{
		UserID:      "user-1",
		WorkspaceID: "ws-1",
		MessageID:   "msg-1",
		Answers: []orchestratorservice.QuestionAnswerInput{
			{QuestionID: "t1q1", OptionIDs: []string{"A"}},
		},
	}

	_, mErr := svc.RespondToQuestions(ctxWithNilSession(), opts)
	if mErr == nil {
		t.Fatal("expected auth/ownership error, got nil")
	}
}

// noop repo stubs to satisfy Service fields not used in actions tests.

type noopToolsStore struct{}

func (noopToolsStore) Seed(_ context.Context, _ orchestratorrepo.SessionRunner, _ []orchestratorrepo.ToolRow) *merrors.Error {
	return nil
}

type noopAgentSettingsStore struct{}

func (noopAgentSettingsStore) GetByAgentID(_ context.Context, _ orchestratorrepo.SessionRunner, _ string) (*orchestratorrepo.AgentSettingsRow, *merrors.Error) {
	return nil, nil
}

func (noopAgentSettingsStore) Save(_ context.Context, _ orchestratorrepo.SessionRunner, _ *orchestratorrepo.AgentSettingsRow) *merrors.Error {
	return nil
}

type noopAgentPromptsStore struct{}

func (noopAgentPromptsStore) ListByAgentID(_ context.Context, _ orchestratorrepo.SessionRunner, _ string) ([]orchestratorrepo.AgentPromptRow, *merrors.Error) {
	return nil, nil
}

func (noopAgentPromptsStore) BulkSave(_ context.Context, _ orchestratorrepo.SessionRunner, _ []orchestratorrepo.AgentPromptRow) *merrors.Error {
	return nil
}

type noopAgentHistoryStore struct{}

func (noopAgentHistoryStore) ListByAgentID(_ context.Context, _ orchestratorrepo.SessionRunner, _ string, _, _ int) ([]orchestratorrepo.AgentHistoryRow, *merrors.Error) {
	return nil, nil
}

func (noopAgentHistoryStore) CountByAgentID(_ context.Context, _ orchestratorrepo.SessionRunner, _ string) (int, *merrors.Error) {
	return 0, nil
}

func (noopAgentHistoryStore) Create(_ context.Context, _ orchestratorrepo.SessionRunner, _ *orchestratorrepo.AgentHistoryRow) *merrors.Error {
	return nil
}

type noopUsageRepo struct{}

func (noopUsageRepo) CheckQuota(_ context.Context, _ orchestratorrepo.SessionRunner, _, _ string) (int64, int64, *merrors.Error) {
	return 0, 0, nil
}

func (noopUsageRepo) IncrementUsage(_ context.Context, _ orchestratorrepo.SessionRunner, _ *usagerepo.IncrementUsageOpts) *merrors.Error {
	return nil
}

func (noopUsageRepo) IncrementAgentUsage(_ context.Context, _ orchestratorrepo.SessionRunner, _ *usagerepo.IncrementAgentUsageOpts) *merrors.Error {
	return nil
}

func (noopUsageRepo) GetAgentUsage(_ context.Context, _ orchestratorrepo.SessionRunner, _ string) (*usagerepo.AgentUsageRow, *merrors.Error) {
	return nil, nil
}

func (noopUsageRepo) GetUserUsage(_ context.Context, _ orchestratorrepo.SessionRunner, _, _ string) (*usagerepo.UserUsageRow, *merrors.Error) {
	return nil, nil
}

func (noopUsageRepo) GetWorkspaceUsage(_ context.Context, _ orchestratorrepo.SessionRunner, _ string) (*usagerepo.WorkspaceUsageRow, *merrors.Error) {
	return nil, nil
}
