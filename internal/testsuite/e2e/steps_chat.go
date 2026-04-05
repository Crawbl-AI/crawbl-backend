package e2e

import (
	"fmt"

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
	if _, err := tc.doRequest("GET", "/v1/workspaces/"+state.workspaceID+"/conversations/"+state.currentConversation, alias, nil); err != nil {
		return err
	}
	if err := tc.assertStatus(200); err != nil {
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
	if _, err := tc.doRequest("GET", "/v1/workspaces/"+state.workspaceID+"/conversations/"+state.currentConversation, alias, nil); err != nil {
		return err
	}
	if err := tc.assertStatus(200); err != nil {
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
	if _, err := tc.doRequest("GET", "/v1/workspaces/"+state.workspaceID+"/conversations/"+state.currentConversation+"/messages", alias, nil); err != nil {
		return err
	}
	return tc.assertStatus(200)
}

func (tc *testContext) currentConversationShouldExposePaginationMetadata() error {
	if err := tc.assertStatus(200); err != nil {
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
	body := map[string]any{
		"local_id": tc.nextLocalID(alias, "mention"),
		"content":  map[string]any{"type": "text", "text": text},
		"attachments": []any{},
		"mentions": []map[string]string{
			{"id": agentID, "name": agentName, "type": "agent"},
		},
	}
	_, err = tc.doRequest("POST", "/v1/workspaces/"+state.workspaceID+"/conversations/"+state.currentConversation+"/messages", alias, body)
	return err
}

// --- Reply assertions ------------------------------------------------

func (tc *testContext) assistantReplyShouldSucceed() error { return tc.assertStatus(200) }

func (tc *testContext) assistantReplyShouldContainText() error {
	return tc.assertJSONNotEmpty("data.0.content.text")
}

func (tc *testContext) assistantReplyShouldComeFromAgent() error {
	return tc.assertJSONNotEmpty("data.0.agent.id")
}

func (tc *testContext) assistantReplyShouldComeFromSpecificAgent(role string) error {
	return tc.assertJSONEquals("data.0.agent.slug", normalizeKey(role))
}

// --- Conversation CRUD -----------------------------------------------

func (tc *testContext) userCreatesConversation(alias, title string) error {
	if err := tc.ensureDefaultWorkspace(alias); err != nil {
		return err
	}
	state := tc.userState(alias)
	body := map[string]any{"title": title, "type": "swarm"}
	if _, err := tc.doRequest("POST", "/v1/workspaces/"+state.workspaceID+"/conversations", alias, body); err != nil {
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
		return fmt.Errorf("no current conversation set for %q", alias)
	}
	_, err := tc.doRequest("POST", "/v1/workspaces/"+state.workspaceID+"/conversations/"+state.currentConversation+"/read", alias, nil)
	return err
}

func (tc *testContext) userSearchesMessages(alias, query string) error {
	state := tc.userState(alias)
	if state.currentConversation == "" {
		return fmt.Errorf("no current conversation set for %q", alias)
	}
	_, err := tc.doRequest("GET", "/v1/workspaces/"+state.workspaceID+"/conversations/"+state.currentConversation+"/messages/search?q="+query, alias, nil)
	return err
}

func (tc *testContext) userDeletesConversation(alias string) error {
	state := tc.userState(alias)
	if state.currentConversation == "" {
		return fmt.Errorf("no current conversation set for %q", alias)
	}
	_, err := tc.doRequest("DELETE", "/v1/workspaces/"+state.workspaceID+"/conversations/"+state.currentConversation, alias, map[string]any{})
	return err
}

func (tc *testContext) userOpensCurrentConversationAgain(alias string) error {
	state := tc.userState(alias)
	if state.currentConversation == "" {
		return fmt.Errorf("no current conversation set for %q", alias)
	}
	_, err := tc.doRequest("GET", "/v1/workspaces/"+state.workspaceID+"/conversations/"+state.currentConversation, alias, nil)
	return err
}
