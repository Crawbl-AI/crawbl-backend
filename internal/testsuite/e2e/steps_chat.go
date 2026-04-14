package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	mobilev1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mobile/v1"
	"github.com/cucumber/godog"
	"github.com/tidwall/gjson"
)

func registerChatSteps(sc *godog.ScenarioContext, tc *testContext) {
	// Conversation browsing
	sc.Step(`^user "([^"]*)" opens the agents in their default workspace$`, tc.userOpensAgents)
	sc.Step(`^user "([^"]*)" should see the default agents$`, tc.userShouldSeeDefaultAgents)
	sc.Step(`^user "([^"]*)" opens the conversations in their default workspace$`, tc.userOpensConversations)
	sc.Step(`^user "([^"]*)" should see the default conversations$`, tc.userShouldSeeDefaultConversations)
	sc.Step(`^user "([^"]*)" opens the swarm conversation$`, tc.userOpensSwarmConversation)
	sc.Step(`^user "([^"]*)" opens the "([^"]*)" direct conversation$`, tc.userOpensDirectConversation)
	sc.Step(`^the current conversation should belong to the "([^"]*)" agent$`, tc.currentConversationShouldBelongToAgent)
	sc.Step(`^user "([^"]*)" opens the messages in the current conversation$`, tc.userOpensMessagesInCurrentConversation)
	sc.Step(`^the current conversation should expose pagination metadata$`, tc.currentConversationShouldExposePaginationMetadata)

	// Messaging
	sc.Step(`^user "([^"]*)" sends the message "([^"]*)" in the current conversation$`, tc.userSendsMessageInCurrentConversation)
	sc.Step(`^user "([^"]*)" sends an empty message in the current conversation$`, tc.userSendsEmptyMessageInCurrentConversation)
	sc.Step(`^user "([^"]*)" mentions the "([^"]*)" agent in the swarm conversation saying "([^"]*)"$`, tc.userMentionsAgentInSwarmConversation)

	// Reply assertions
	sc.Step(`^the assistant reply should succeed$`, tc.assistantReplyShouldSucceed)
	sc.Step(`^the assistant reply should contain text$`, tc.assistantReplyShouldContainText)
	sc.Step(`^the assistant reply should come from an agent$`, tc.assistantReplyShouldComeFromAgent)
	sc.Step(`^the assistant reply should come from the "([^"]*)" agent$`, tc.assistantReplyShouldComeFromSpecificAgent)
	sc.Step(`^the assistant reply should mention "([^"]*)"$`, tc.assistantReplyShouldMention)
	sc.Step(`^the assistant reply should mention one of "([^"]*)"$`, tc.assistantReplyShouldMentionOneOf)

	// Conversation CRUD
	sc.Step(`^user "([^"]*)" creates a conversation named "([^"]*)" in their default workspace$`, tc.userCreatesConversation)
	sc.Step(`^user "([^"]*)" marks the current conversation as read$`, tc.userMarksConversationRead)
	sc.Step(`^user "([^"]*)" searches messages for "([^"]*)" in the current conversation$`, tc.userSearchesMessages)
	sc.Step(`^user "([^"]*)" deletes the current conversation$`, tc.userDeletesConversation)
	sc.Step(`^user "([^"]*)" opens the current conversation again$`, tc.userOpensCurrentConversationAgain)

	// Edge cases
	sc.Step(`^user "([^"]*)" opens a workspace that does not exist$`, tc.userOpensMissingWorkspace)
	sc.Step(`^user "([^"]*)" opens a conversation that does not exist in their default workspace$`, tc.userOpensMissingConversation)
}

// --- Browsing --------------------------------------------------------

func (tc *testContext) userOpensAgents(alias string) error {
	return tc.fetchAgents(alias)
}

func (tc *testContext) userShouldSeeDefaultAgents(alias string) error {
	if err := tc.userOpensAgents(alias); err != nil {
		return err
	}
	if err := tc.assertJSONArrayLength("data", 3); err != nil {
		return err
	}
	for _, slug := range []string{"manager", "wally", "eve"} {
		if err := tc.assertJSONArrayContainsItem("data", "slug", slug); err != nil {
			return err
		}
	}
	return nil
}

func (tc *testContext) userOpensConversations(alias string) error {
	return tc.fetchConversations(alias)
}

func (tc *testContext) userShouldSeeDefaultConversations(alias string) error {
	if err := tc.userOpensConversations(alias); err != nil {
		return err
	}
	// At least 3 conversations (swarm + 2 agent directs); may be more
	// if earlier CRUD scenarios created extras.
	if err := tc.assertJSONArrayMinLength("data", 3); err != nil {
		return err
	}
	if err := tc.assertJSONArrayContainsItem("data", "type", "swarm"); err != nil {
		return err
	}
	if err := tc.assertJSONArrayContainsItem("data", "title", "Wally"); err != nil {
		return err
	}
	return tc.assertJSONArrayContainsItem("data", "title", "Eve")
}

func (tc *testContext) userOpensSwarmConversation(alias string) error {
	if err := tc.ensureConversationCatalog(alias); err != nil {
		return err
	}
	state := tc.userState(alias)
	state.currentConversation = state.swarmConversationID
	if _, err := tc.doRequest("GET", pathWorkspaces+state.workspaceID+pathConversations+state.currentConversation, alias, nil); err != nil {
		return err
	}
	if err := tc.assertStatus(http.StatusOK); err != nil {
		return err
	}
	return tc.assertJSONEquals("data.type", "swarm")
}

func (tc *testContext) userOpensDirectConversation(alias, role string) error {
	if err := tc.ensureConversationCatalog(alias); err != nil {
		return err
	}
	state := tc.userState(alias)
	key := normalizeKey(role)
	convID := state.conversationIDsByKey[key]
	if convID == "" {
		return fmt.Errorf("no direct conversation found for %q", role)
	}
	state.currentConversation = convID
	if _, err := tc.doRequest("GET", pathWorkspaces+state.workspaceID+pathConversations+state.currentConversation, alias, nil); err != nil {
		return err
	}
	if err := tc.assertStatus(http.StatusOK); err != nil {
		return err
	}
	return tc.assertJSONEquals("data.type", "agent")
}

func (tc *testContext) currentConversationShouldBelongToAgent(role string) error {
	return tc.assertJSONEquals("data.agent.slug", normalizeKey(role))
}

func (tc *testContext) userOpensMessagesInCurrentConversation(alias string) error {
	state := tc.userState(alias)
	if state.currentConversation == "" {
		if err := tc.userOpensSwarmConversation(alias); err != nil {
			return err
		}
	}
	if _, err := tc.doRequest("GET", pathWorkspaces+state.workspaceID+pathConversations+state.currentConversation+pathMessages, alias, nil); err != nil {
		return err
	}
	return tc.assertStatus(http.StatusOK)
}

func (tc *testContext) currentConversationShouldExposePaginationMetadata() error {
	if err := tc.assertStatus(http.StatusOK); err != nil {
		return err
	}
	for _, field := range []string{"data.pagination.has_next", "data.pagination.has_prev"} {
		if !gjson.GetBytes(tc.lastBody, field).Exists() {
			return fmt.Errorf("%s missing from response", field)
		}
	}
	return nil
}

// --- Messaging -------------------------------------------------------

func (tc *testContext) userSendsMessageInCurrentConversation(alias, text string) error {
	return tc.sendMessage(alias, text)
}

func (tc *testContext) userSendsEmptyMessageInCurrentConversation(alias string) error {
	return tc.sendMessage(alias, "")
}

func (tc *testContext) userMentionsAgentInSwarmConversation(alias, role, text string) error {
	if err := tc.ensureConversationCatalog(alias); err != nil {
		return err
	}
	state := tc.userState(alias)
	state.currentConversation = state.swarmConversationID
	agentID, err := tc.agentIDForSlug(alias, role)
	if err != nil {
		return err
	}
	agentName := state.agentNamesBySlug[normalizeKey(role)]
	body := &mobilev1.SendMessageRequest{
		LocalId: tc.nextLocalID(alias, "mention"),
		Content: &mobilev1.MessageContentPayload{Type: "text", Text: text},
		Mentions: []*mobilev1.MentionPayload{
			{AgentId: agentID, AgentName: agentName, Offset: 0, Length: int64(len(agentName) + 1)},
		},
	}
	if _, err = tc.doProtoRequest("POST", pathWorkspaces+state.workspaceID+pathConversations+state.currentConversation+pathMessages, alias, body); err != nil {
		return err
	}
	if tc.lastStatus != http.StatusOK && tc.lastStatus != http.StatusCreated {
		return nil
	}
	userMsgID := gjson.GetBytes(tc.lastBody, "data.id").String()
	userMsgCreated := gjson.GetBytes(tc.lastBody, "data.created_at").String()
	if userMsgID == "" {
		return fmt.Errorf("mention: response body missing data.id: %s", abbreviatedBody(tc.lastBody))
	}
	return tc.pollForAssistantReply(alias, userMsgID, userMsgCreated)
}

// --- Reply assertions ------------------------------------------------

func (tc *testContext) assistantReplyShouldSucceed() error { return tc.assertStatus(http.StatusOK) }

func (tc *testContext) assistantReplyShouldContainText() error {
	combined := collectReplyText(tc.lastBody)
	if combined == "" {
		return fmt.Errorf("JSON data.0.content.text: expected non-empty value")
	}
	return nil
}

func (tc *testContext) assistantReplyShouldComeFromAgent() error {
	return tc.assertJSONNotEmpty("data.0.agent.id")
}

// assistantReplyShouldComeFromSpecificAgent checks that the first
// assistant reply came from the expected agent. Because the LLM may
// initially route through the manager before the delegated agent's
// reply surfaces, this step retries up to 3 times with a 2-second
// backoff, re-fetching the conversation messages each attempt.
func (tc *testContext) assistantReplyShouldComeFromSpecificAgent(role string) error {
	expected := normalizeKey(role)
	const maxAttempts = 3
	const retryDelay = 2 * time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if replyContainsAgent(tc.lastBody, expected) {
			return nil
		}
		if attempt == maxAttempts {
			got := gjson.GetBytes(tc.lastBody, "data.0.agent.slug").String()
			return fmt.Errorf("JSON data.*.agent.slug: expected %q in any reply turn, got first=%q after %d attempts", expected, got, maxAttempts)
		}
		if err := tc.refetchReplies(retryDelay); err != nil {
			return err
		}
	}
	return nil
}

// replyContainsAgent checks whether any reply turn was produced by the named agent.
func replyContainsAgent(body []byte, slug string) bool {
	for _, r := range gjson.GetBytes(body, "data").Array() {
		if r.Get("agent.slug").String() == slug {
			return true
		}
	}
	return false
}

// refetchReplies re-fetches conversation messages after a delay and rebuilds
// tc.lastBody in the synthesized reply shape for downstream assertions.
func (tc *testContext) refetchReplies(delay time.Duration) error {
	time.Sleep(delay)
	state := tc.userState("primary")
	listURL := pathWorkspaces + state.workspaceID + pathConversations + state.currentConversation + pathMessages
	if _, err := tc.doRequest("GET", listURL, "primary", nil); err != nil {
		return fmt.Errorf("retry fetch messages: %w", err)
	}
	msgs := gjson.GetBytes(tc.lastBody, "data.messages").Array()
	var assistantReplies []gjson.Result
	for _, m := range msgs {
		if r := m.Get("role").String(); r != "" && r != "user" {
			assistantReplies = append(assistantReplies, m)
		}
	}
	if len(assistantReplies) > 0 {
		synthesized, err := synthesizeRepliesBody(assistantReplies)
		if err != nil {
			return fmt.Errorf("retry synthesize: %w", err)
		}
		tc.lastBody = synthesized
		tc.lastStatus = http.StatusOK
	}
	return nil
}

// assistantReplyShouldMention verifies the assistant's reply contains
// the given keyword (case-insensitive substring match). Looks across
// the whole `data` array so it still works when the reply came from a
// swarm scenario that yields multiple agent turns — the expected
// keyword must appear in at least one turn's text.
func (tc *testContext) assistantReplyShouldMention(keyword string) error {
	combined := collectReplyText(tc.lastBody)
	if combined == "" {
		return fmt.Errorf("assistant reply is empty, expected mention of %q", keyword)
	}
	if !strings.Contains(strings.ToLower(combined), strings.ToLower(keyword)) {
		return fmt.Errorf("assistant reply does not mention %q: %q", keyword, combined)
	}
	return nil
}

// assistantReplyShouldMentionOneOf verifies the reply contains at least
// one of the pipe-separated keywords. Use when the expected wording is
// flexible (e.g., "three thousand" vs "3000" vs "3,000 euros").
func (tc *testContext) assistantReplyShouldMentionOneOf(csv string) error {
	combined := strings.ToLower(collectReplyText(tc.lastBody))
	if combined == "" {
		return fmt.Errorf("assistant reply is empty, expected one of %q", csv)
	}
	for _, kw := range strings.Split(csv, "|") {
		kw = strings.TrimSpace(strings.ToLower(kw))
		if kw == "" {
			continue
		}
		if strings.Contains(combined, kw) {
			return nil
		}
	}
	return fmt.Errorf("assistant reply mentions none of %q: %q", csv, combined)
}

// collectReplyText concatenates every turn's content.text from a
// SendMessage response body. Returning them joined means a single
// substring check can span a swarm reply that split across multiple
// agent turns.
func collectReplyText(body []byte) string {
	var parts []string
	for _, turn := range gjson.GetBytes(body, "data").Array() {
		if t := turn.Get("content.text").String(); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, "\n")
}

// --- Conversation CRUD -----------------------------------------------

func (tc *testContext) userCreatesConversation(alias, title string) error {
	if err := tc.ensureDefaultWorkspace(alias); err != nil {
		return err
	}
	state := tc.userState(alias)
	body := &mobilev1.CreateConversationRequest{Type: "swarm"}
	if _, err := tc.doProtoRequest("POST", pathWorkspaces+state.workspaceID+"/conversations", alias, body); err != nil {
		return err
	}
	id := gjson.GetBytes(tc.lastBody, "data.id").String()
	if id != "" {
		state.currentConversation = id
	}
	return nil
}

func (tc *testContext) userMarksConversationRead(alias string) error {
	state := tc.userState(alias)
	if state.currentConversation == "" {
		return fmt.Errorf(errNoConversation, alias)
	}
	_, err := tc.doRequest("POST", pathWorkspaces+state.workspaceID+pathConversations+state.currentConversation+"/read", alias, nil)
	return err
}

func (tc *testContext) userSearchesMessages(alias, query string) error {
	state := tc.userState(alias)
	if state.currentConversation == "" {
		return fmt.Errorf(errNoConversation, alias)
	}
	_, err := tc.doRequest("GET", pathWorkspaces+state.workspaceID+pathConversations+state.currentConversation+pathMessages+"/search?q="+query, alias, nil)
	return err
}

func (tc *testContext) userDeletesConversation(alias string) error {
	state := tc.userState(alias)
	if state.currentConversation == "" {
		return fmt.Errorf(errNoConversation, alias)
	}
	_, err := tc.doRequest("DELETE", pathWorkspaces+state.workspaceID+pathConversations+state.currentConversation, alias, nil)
	return err
}

func (tc *testContext) userOpensCurrentConversationAgain(alias string) error {
	state := tc.userState(alias)
	if state.currentConversation == "" {
		return fmt.Errorf(errNoConversation, alias)
	}
	_, err := tc.doRequest("GET", pathWorkspaces+state.workspaceID+pathConversations+state.currentConversation, alias, nil)
	return err
}
