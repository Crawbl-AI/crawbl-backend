package mcpservice

import (
	"context"
	"strings"
	"testing"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/realtime"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/artifactrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/mcprepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/workflowrepo"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// fakeWorkspaceStore satisfies workspaceGetter for tests.
type fakeWorkspaceStore struct {
	ws  *orchestrator.Workspace
	err *merrors.Error
}

func (f *fakeWorkspaceStore) GetByID(_ context.Context, _ orchestratorrepo.SessionRunner, _, _ string) (*orchestrator.Workspace, *merrors.Error) {
	return f.ws, f.err
}

// fakeConversationStore satisfies conversationStore for tests.
type fakeConversationStore struct {
	conv *orchestrator.Conversation
	err  *merrors.Error
}

func (f *fakeConversationStore) ListByWorkspaceID(_ context.Context, _ orchestratorrepo.SessionRunner, _ string) ([]*orchestrator.Conversation, *merrors.Error) {
	return nil, nil
}

func (f *fakeConversationStore) GetByID(_ context.Context, _ orchestratorrepo.SessionRunner, _, _ string) (*orchestrator.Conversation, *merrors.Error) {
	return f.conv, f.err
}

// fakeAgentStore satisfies agentStore for tests.
type fakeAgentStore struct {
	agents []*orchestrator.Agent
	err    *merrors.Error
}

func (f *fakeAgentStore) GetByIDGlobal(_ context.Context, _ orchestratorrepo.SessionRunner, _ string) (*orchestrator.Agent, *merrors.Error) {
	if len(f.agents) > 0 {
		return f.agents[0], f.err
	}
	return nil, f.err
}

func (f *fakeAgentStore) ListByWorkspaceID(_ context.Context, _ orchestratorrepo.SessionRunner, _ string) ([]*orchestrator.Agent, *merrors.Error) {
	return f.agents, f.err
}

// fakeAgentHistoryStore satisfies agentHistoryCreator for tests.
type fakeAgentHistoryStore struct{}

func (f *fakeAgentHistoryStore) Create(_ context.Context, _ orchestratorrepo.SessionRunner, _ *orchestratorrepo.AgentHistoryRow) *merrors.Error {
	return nil
}

// fakeMessageStore satisfies messageStore and records saved messages.
type fakeMessageStore struct {
	saved []*orchestrator.Message
	err   *merrors.Error
}

func (f *fakeMessageStore) ListRecent(_ context.Context, _ orchestratorrepo.SessionRunner, _ string, _ int) ([]*orchestrator.Message, *merrors.Error) {
	return nil, nil
}

func (f *fakeMessageStore) Save(_ context.Context, _ orchestratorrepo.SessionRunner, msg *orchestrator.Message) *merrors.Error {
	if f.err != nil {
		return f.err
	}
	f.saved = append(f.saved, msg)
	return nil
}

// spyBroadcaster records EmitMessageNew calls.
type spyBroadcaster struct {
	realtime.NopBroadcaster
	newCalls     int
	lastWorkspID string
	lastMsg      any
}

func (s *spyBroadcaster) EmitMessageNew(_ context.Context, workspaceID string, data any) {
	s.newCalls++
	s.lastWorkspID = workspaceID
	s.lastMsg = data
}

// noopLogger satisfies the logger interface.
type noopLogger struct{}

func (noopLogger) Info(_ string, _ ...any)                            {}
func (noopLogger) Warn(_ string, _ ...any)                            {}
func (noopLogger) Error(_ string, _ ...any)                           {}
func (noopLogger) InfoContext(_ context.Context, _ string, _ ...any)  {}
func (noopLogger) ErrorContext(_ context.Context, _ string, _ ...any) {}

// buildTestService assembles a minimal service wired with the provided fakes.
func buildTestService(
	ws workspaceGetter,
	conv conversationStore,
	agents agentStore,
	msgs *fakeMessageStore,
	broadcaster realtime.Broadcaster,
) Service {
	repos := Repos{
		MCP:          mcprepo.New(),
		Workspace:    ws,
		Conversation: conv,
		Agent:        agents,
		AgentHistory: &fakeAgentHistoryStore{},
		Message:      msgs,
		Artifact:     artifactrepo.New(),
		Workflow:     workflowrepo.New(),
	}
	infra := Infra{
		Logger:      noopLogger{},
		Broadcaster: broadcaster,
	}
	svc, err := New(repos, infra, nil, nil)
	if err != nil {
		panic("buildTestService: " + err.Error())
	}
	return svc
}

// happyWorkspace returns a workspace store that always succeeds.
func happyWorkspace() *fakeWorkspaceStore {
	return &fakeWorkspaceStore{ws: &orchestrator.Workspace{ID: "ws-1"}}
}

// happyConversation returns a conversation store that always succeeds.
func happyConversation() *fakeConversationStore {
	return &fakeConversationStore{conv: &orchestrator.Conversation{ID: "conv-1", WorkspaceID: "ws-1"}}
}

// agentWithSlug returns an agent store containing one agent with the given slug.
func agentWithSlug(slug, id string) *fakeAgentStore {
	return &fakeAgentStore{agents: []*orchestrator.Agent{{ID: id, Slug: slug}}}
}

// assertBroadcast checks that EmitMessageNew was called exactly once for the given workspace.
func assertBroadcast(t *testing.T, spy *spyBroadcaster, wantWorkspaceID string) {
	t.Helper()
	if spy.newCalls != 1 {
		t.Fatalf("EmitMessageNew called %d times, want 1", spy.newCalls)
	}
	if spy.lastWorkspID != wantWorkspaceID {
		t.Fatalf("EmitMessageNew workspace = %q, want %q", spy.lastWorkspID, wantWorkspaceID)
	}
}

// assertSavedMessage checks that exactly one message was saved and validates its top-level fields.
func assertSavedMessage(t *testing.T, msgs *fakeMessageStore) *orchestrator.Message {
	t.Helper()
	if len(msgs.saved) != 1 {
		t.Fatalf("expected 1 saved message, got %d", len(msgs.saved))
	}
	msg := msgs.saved[0]
	if msg.Content.Type != orchestrator.MessageContentTypeQuestions {
		t.Fatalf("content type = %q, want %q", msg.Content.Type, orchestrator.MessageContentTypeQuestions)
	}
	if msg.Role != orchestrator.MessageRoleAgent {
		t.Fatalf("role = %q, want %q", msg.Role, orchestrator.MessageRoleAgent)
	}
	if msg.Status != orchestrator.MessageStatusDelivered {
		t.Fatalf("status = %q, want %q", msg.Status, orchestrator.MessageStatusDelivered)
	}
	if msg.AgentID == nil || *msg.AgentID == "" {
		t.Fatal("expected AgentID to be set")
	}
	return msg
}

// assertTurnShape checks the turn count and the first turn's index and question count.
func assertTurnShape(t *testing.T, msg *orchestrator.Message) orchestrator.QuestionTurn {
	t.Helper()
	if len(msg.Content.Turns) != 1 {
		t.Fatalf("turns count = %d, want 1", len(msg.Content.Turns))
	}
	turn := msg.Content.Turns[0]
	if turn.Index != 1 {
		t.Fatalf("turn.Index = %d, want 1", turn.Index)
	}
	if len(turn.Questions) != 2 {
		t.Fatalf("questions count = %d, want 2", len(turn.Questions))
	}
	return turn
}

// assertOptionIDs checks that each option's ID matches the expected list.
func assertOptionIDs(t *testing.T, label string, q orchestrator.QuestionItem, wantIDs []string) {
	t.Helper()
	if len(q.Options) != len(wantIDs) {
		t.Fatalf("%s option count = %d, want %d", label, len(q.Options), len(wantIDs))
	}
	for i, o := range q.Options {
		if o.ID != wantIDs[i] {
			t.Fatalf("%s option[%d].ID = %q, want %q", label, i, o.ID, wantIDs[i])
		}
	}
}

func TestAskQuestions_HappyPath(t *testing.T) {
	t.Parallel()

	spy := &spyBroadcaster{}
	msgs := &fakeMessageStore{}
	svc := buildTestService(happyWorkspace(), happyConversation(), agentWithSlug("bot", "agent-42"), msgs, spy)

	params := &AskQuestionsParams{
		AgentSlug:      "bot",
		ConversationId: "conv-1",
		Turns: []*AskQuestionsTurn{
			{
				Questions: []*AskQuestionsQuestion{
					{Prompt: "Pick a colour", Mode: string(orchestrator.QuestionModeSingle), Options: []string{"Red", "Green", "Blue"}, AllowCustom: true},
					{Prompt: "Pick fruits", Mode: string(orchestrator.QuestionModeMulti), Options: []string{"Apple", "Banana", "Cherry"}, AllowCustom: true},
				},
			},
		},
	}

	result, err := svc.AskQuestions(context.Background(), nil, "user-1", "ws-1", params)
	if err != nil {
		t.Fatalf("AskQuestions returned unexpected error: %v", err)
	}
	if result.MessageId == "" {
		t.Fatal("expected non-empty MessageId")
	}

	assertBroadcast(t, spy, "ws-1")

	msg := assertSavedMessage(t, msgs)
	turn := assertTurnShape(t, msg)

	q1 := turn.Questions[0]
	if q1.ID != "t1q1" {
		t.Fatalf("q1.ID = %q, want %q", q1.ID, "t1q1")
	}
	if q1.Mode != orchestrator.QuestionModeSingle {
		t.Fatalf("q1.Mode = %q, want %q", q1.Mode, orchestrator.QuestionModeSingle)
	}
	if !q1.AllowCustom {
		t.Fatal("q1.AllowCustom should be true")
	}
	assertOptionIDs(t, "q1", q1, []string{"A", "B", "C"})

	q2 := turn.Questions[1]
	if q2.ID != "t1q2" {
		t.Fatalf("q2.ID = %q, want %q", q2.ID, "t1q2")
	}
	if q2.Mode != orchestrator.QuestionModeMulti {
		t.Fatalf("q2.Mode = %q, want %q", q2.Mode, orchestrator.QuestionModeMulti)
	}
	if !q2.AllowCustom {
		t.Fatal("q2.AllowCustom should be true")
	}
}

func TestAskQuestions_ValidationErrors(t *testing.T) {
	t.Parallel()

	svc := buildTestService(happyWorkspace(), happyConversation(), agentWithSlug("bot", "agent-42"), &fakeMessageStore{}, realtime.NopBroadcaster{})

	twoOptions := []string{"Yes", "No"}

	tests := []struct {
		name   string
		params *AskQuestionsParams
	}{
		{
			name: "zero turns",
			params: &AskQuestionsParams{
				AgentSlug:      "bot",
				ConversationId: "conv-1",
				Turns:          nil,
			},
		},
		{
			name: "turn with zero questions",
			params: &AskQuestionsParams{
				AgentSlug:      "bot",
				ConversationId: "conv-1",
				Turns:          []*AskQuestionsTurn{{Questions: nil}},
			},
		},
		{
			name: "question with fewer than two options",
			params: &AskQuestionsParams{
				AgentSlug:      "bot",
				ConversationId: "conv-1",
				Turns: []*AskQuestionsTurn{{
					Questions: []*AskQuestionsQuestion{
						{Prompt: "Q?", Mode: string(orchestrator.QuestionModeSingle), Options: []string{"OnlyOne"}},
					},
				}},
			},
		},
		{
			name: "question with more than 26 options",
			params: &AskQuestionsParams{
				AgentSlug:      "bot",
				ConversationId: "conv-1",
				Turns: []*AskQuestionsTurn{{
					Questions: []*AskQuestionsQuestion{
						{Prompt: "Q?", Mode: string(orchestrator.QuestionModeSingle), Options: func() []string {
							opts := make([]string, 27)
							for i := range opts {
								opts[i] = "opt"
							}
							return opts
						}()},
					},
				}},
			},
		},
		{
			name: "mode neither single nor multi",
			params: &AskQuestionsParams{
				AgentSlug:      "bot",
				ConversationId: "conv-1",
				Turns: []*AskQuestionsTurn{{
					Questions: []*AskQuestionsQuestion{
						{Prompt: "Q?", Mode: "invalid", Options: twoOptions},
					},
				}},
			},
		},
		{
			name: "empty prompt",
			params: &AskQuestionsParams{
				AgentSlug:      "bot",
				ConversationId: "conv-1",
				Turns: []*AskQuestionsTurn{{
					Questions: []*AskQuestionsQuestion{
						{Prompt: "   ", Mode: string(orchestrator.QuestionModeSingle), Options: twoOptions},
					},
				}},
			},
		},
		{
			name: "empty option label",
			params: &AskQuestionsParams{
				AgentSlug:      "bot",
				ConversationId: "conv-1",
				Turns: []*AskQuestionsTurn{{
					Questions: []*AskQuestionsQuestion{
						{Prompt: "Q?", Mode: string(orchestrator.QuestionModeSingle), Options: []string{"Valid", ""}},
					},
				}},
			},
		},
		{
			name: "too many turns",
			params: &AskQuestionsParams{
				AgentSlug:      "bot",
				ConversationId: "conv-1",
				Turns: func() []*AskQuestionsTurn {
					turns := make([]*AskQuestionsTurn, maxTurnsPerMessage+1)
					for i := range turns {
						turns[i] = &AskQuestionsTurn{
							Questions: []*AskQuestionsQuestion{
								{Prompt: "Q?", Mode: string(orchestrator.QuestionModeSingle), Options: twoOptions},
							},
						}
					}
					return turns
				}(),
			},
		},
		{
			name: "too many questions in a turn",
			params: &AskQuestionsParams{
				AgentSlug:      "bot",
				ConversationId: "conv-1",
				Turns: []*AskQuestionsTurn{{
					Questions: func() []*AskQuestionsQuestion {
						qs := make([]*AskQuestionsQuestion, maxQuestionsPerTurn+1)
						for i := range qs {
							qs[i] = &AskQuestionsQuestion{
								Prompt: "Q?", Mode: string(orchestrator.QuestionModeSingle), Options: twoOptions,
							}
						}
						return qs
					}(),
				}},
			},
		},
		{
			name: "prompt too long",
			params: &AskQuestionsParams{
				AgentSlug:      "bot",
				ConversationId: "conv-1",
				Turns: []*AskQuestionsTurn{{
					Questions: []*AskQuestionsQuestion{
						{
							Prompt:  strings.Repeat("x", maxPromptLen+1),
							Mode:    string(orchestrator.QuestionModeSingle),
							Options: twoOptions,
						},
					},
				}},
			},
		},
		{
			name: "option label too long",
			params: &AskQuestionsParams{
				AgentSlug:      "bot",
				ConversationId: "conv-1",
				Turns: []*AskQuestionsTurn{{
					Questions: []*AskQuestionsQuestion{
						{
							Prompt:  "Q?",
							Mode:    string(orchestrator.QuestionModeSingle),
							Options: []string{"ok", strings.Repeat("y", maxOptionLabelLen+1)},
						},
					},
				}},
			},
		},
		{
			name: "turn label too long",
			params: &AskQuestionsParams{
				AgentSlug:      "bot",
				ConversationId: "conv-1",
				Turns: []*AskQuestionsTurn{{
					Label: strings.Repeat("z", maxTurnLabelLen+1),
					Questions: []*AskQuestionsQuestion{
						{Prompt: "Q?", Mode: string(orchestrator.QuestionModeSingle), Options: twoOptions},
					},
				}},
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := svc.AskQuestions(context.Background(), nil, "user-1", "ws-1", tc.params)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
		})
	}
}

func TestAskQuestions_UnknownAgentSlug(t *testing.T) {
	t.Parallel()

	emptyAgents := &fakeAgentStore{agents: []*orchestrator.Agent{}}
	svc := buildTestService(happyWorkspace(), happyConversation(), emptyAgents, &fakeMessageStore{}, realtime.NopBroadcaster{})

	params := &AskQuestionsParams{
		AgentSlug:      "ghost",
		ConversationId: "conv-1",
		Turns: []*AskQuestionsTurn{{
			Questions: []*AskQuestionsQuestion{
				{Prompt: "Q?", Mode: string(orchestrator.QuestionModeSingle), Options: []string{"A", "B"}},
			},
		}},
	}

	_, err := svc.AskQuestions(context.Background(), nil, "user-1", "ws-1", params)
	if err == nil {
		t.Fatal("expected error for unknown agent slug, got nil")
	}
}

func TestAskQuestions_ConversationNotFound(t *testing.T) {
	t.Parallel()

	badConv := &fakeConversationStore{err: merrors.ErrConversationNotFound}
	svc := buildTestService(happyWorkspace(), badConv, agentWithSlug("bot", "agent-42"), &fakeMessageStore{}, realtime.NopBroadcaster{})

	params := &AskQuestionsParams{
		AgentSlug:      "bot",
		ConversationId: "conv-missing",
		Turns: []*AskQuestionsTurn{{
			Questions: []*AskQuestionsQuestion{
				{Prompt: "Q?", Mode: string(orchestrator.QuestionModeSingle), Options: []string{"A", "B"}},
			},
		}},
	}

	_, err := svc.AskQuestions(context.Background(), nil, "user-1", "ws-1", params)
	if err == nil {
		t.Fatal("expected error for missing conversation, got nil")
	}
}
