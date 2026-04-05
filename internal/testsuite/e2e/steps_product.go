package e2e

import (
	"fmt"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"github.com/tidwall/gjson"
)

const (
	defaultRuntimeReadyTimeout = 3 * time.Minute
	defaultRuntimePollInterval = 2 * time.Second
)

type runtimeSnapshot struct {
	Status    string
	Phase     string
	Verified  bool
	LastError string
}

func registerProductSteps(sc *godog.ScenarioContext, tc *testContext) {
	sc.Step(`^the guest checks the service health$`, tc.guestChecksServiceHealth)
	sc.Step(`^the service should report online$`, tc.serviceShouldReportOnline)
	sc.Step(`^the guest reads the public legal documents$`, tc.guestReadsPublicLegalDocuments)
	sc.Step(`^the public legal documents should be available$`, tc.publicLegalDocumentsShouldBeAvailable)

	sc.Step(`^user "([^"]*)" signs up$`, tc.userSignsUp)
	sc.Step(`^user "([^"]*)" signs in$`, tc.userSignsIn)
	sc.Step(`^user "([^"]*)" should exist in the database$`, tc.userShouldExistInDatabase)
	sc.Step(`^user "([^"]*)" should have one workspace in the database$`, tc.userShouldHaveOneWorkspaceInDatabase)
	sc.Step(`^user "([^"]*)" opens their profile$`, tc.userOpensProfile)
	sc.Step(`^user "([^"]*)" should see their default profile details$`, tc.userShouldSeeDefaultProfileDetails)
	sc.Step(`^user "([^"]*)" updates their profile details$`, tc.userUpdatesProfileDetails)
	sc.Step(`^user "([^"]*)" should see their updated profile details$`, tc.userShouldSeeUpdatedProfileDetails)
	sc.Step(`^user "([^"]*)" registers a push token$`, tc.userRegistersPushToken)
	sc.Step(`^the push token for user "([^"]*)" should be stored$`, tc.pushTokenShouldBeStored)

	sc.Step(`^user "([^"]*)" opens their legal status$`, tc.userOpensLegalStatus)
	sc.Step(`^user "([^"]*)" should see the current legal versions$`, tc.userShouldSeeCurrentLegalVersions)
	sc.Step(`^user "([^"]*)" accepts the current legal documents$`, tc.userAcceptsCurrentLegalDocuments)
	sc.Step(`^user "([^"]*)" should show accepted legal documents$`, tc.userShouldShowAcceptedLegalDocuments)

	sc.Step(`^user "([^"]*)" opens their workspace list$`, tc.userOpensWorkspaceList)
	sc.Step(`^user "([^"]*)" should have a single default workspace$`, tc.userShouldHaveSingleDefaultWorkspace)
	sc.Step(`^user "([^"]*)" opens their default workspace$`, tc.userOpensDefaultWorkspace)
	sc.Step(`^user "([^"]*)" should see runtime details for their default workspace$`, tc.userShouldSeeRuntimeDetails)
	sc.Step(`^user "([^"]*)" waits until their assistant is ready$`, tc.userWaitsUntilAssistantIsReady)
	sc.Step(`^user "([^"]*)" should see their workspace runtime as ready$`, tc.userShouldSeeWorkspaceRuntimeReady)

	sc.Step(`^user "([^"]*)" opens the agents in their default workspace$`, tc.userOpensAgents)
	sc.Step(`^user "([^"]*)" should see the default agents$`, tc.userShouldSeeDefaultAgents)
	sc.Step(`^user "([^"]*)" opens the conversations in their default workspace$`, tc.userOpensConversations)
	sc.Step(`^user "([^"]*)" should see the default conversations$`, tc.userShouldSeeDefaultConversations)
	sc.Step(`^user "([^"]*)" opens the swarm conversation$`, tc.userOpensSwarmConversation)
	sc.Step(`^user "([^"]*)" opens the "([^"]*)" direct conversation$`, tc.userOpensDirectConversation)
	sc.Step(`^the current conversation should belong to the "([^"]*)" agent$`, tc.currentConversationShouldBelongToAgent)
	sc.Step(`^user "([^"]*)" opens the messages in the current conversation$`, tc.userOpensMessagesInCurrentConversation)
	sc.Step(`^the current conversation should expose pagination metadata$`, tc.currentConversationShouldExposePaginationMetadata)

	sc.Step(`^user "([^"]*)" sends the message "([^"]*)" in the current conversation$`, tc.userSendsMessageInCurrentConversation)
	sc.Step(`^user "([^"]*)" sends an empty message in the current conversation$`, tc.userSendsEmptyMessageInCurrentConversation)
	sc.Step(`^user "([^"]*)" mentions the "([^"]*)" agent in the swarm conversation saying "([^"]*)"$`, tc.userMentionsAgentInSwarmConversation)
	sc.Step(`^the assistant reply should succeed$`, tc.assistantReplyShouldSucceed)
	sc.Step(`^the assistant reply should contain text$`, tc.assistantReplyShouldContainText)
	sc.Step(`^the assistant reply should come from an agent$`, tc.assistantReplyShouldComeFromAgent)
	sc.Step(`^the assistant reply should come from the "([^"]*)" agent$`, tc.assistantReplyShouldComeFromSpecificAgent)

	sc.Step(`^a guest requests their profile$`, tc.guestRequestsProfile)
	sc.Step(`^user "([^"]*)" opens a workspace that does not exist$`, tc.userOpensMissingWorkspace)
	sc.Step(`^user "([^"]*)" opens a conversation that does not exist in their default workspace$`, tc.userOpensMissingConversation)
	sc.Step(`^the request should be unauthorized$`, tc.requestShouldBeUnauthorized)
	sc.Step(`^the request should be rejected as invalid$`, tc.requestShouldBeRejectedAsInvalid)
	sc.Step(`^the request should be rejected as not found$`, tc.requestShouldBeRejectedAsNotFound)
	sc.Step(`^the deleted account should no longer behave like an active user$`, tc.deletedAccountShouldNoLongerBehaveLikeActiveUser)

	sc.Step(`^users "([^"]*)" and "([^"]*)" should have different default workspaces$`, tc.usersShouldHaveDifferentDefaultWorkspaces)
	sc.Step(`^user "([^"]*)" opens user "([^"]*)"'s default workspace$`, tc.userOpensAnotherUsersWorkspace)
	sc.Step(`^user "([^"]*)" opens user "([^"]*)"'s agents$`, tc.userOpensAnotherUsersAgents)
	sc.Step(`^user "([^"]*)" opens user "([^"]*)"'s conversations$`, tc.userOpensAnotherUsersConversations)
	sc.Step(`^deleting user "([^"]*)" should not affect user "([^"]*)"$`, tc.deletingUserShouldNotAffectUser)

	sc.Step(`^user "([^"]*)" deletes their account$`, tc.userDeletesTheirAccount)
	sc.Step(`^user "([^"]*)" should be marked as deleted in the database$`, tc.userShouldBeMarkedAsDeletedInDatabase)

	sc.Step(`^user "([^"]*)" opens the integrations catalog$`, tc.userOpensIntegrationsCatalog)
	sc.Step(`^user "([^"]*)" should see tool categories$`, tc.userShouldSeeToolCategories)
	sc.Step(`^user "([^"]*)" should see tools in the catalog$`, tc.userShouldSeeToolsInCatalog)
	sc.Step(`^user "([^"]*)" should see integration apps in the catalog$`, tc.userShouldSeeIntegrationAppsInCatalog)
}

func (tc *testContext) guestChecksServiceHealth() error {
	return tc.sendGetNoAuth("/v1/health")
}

func (tc *testContext) serviceShouldReportOnline() error {
	if err := tc.assertStatus(200); err != nil {
		return err
	}
	return tc.assertJSONEquals("data.online", "true")
}

func (tc *testContext) guestReadsPublicLegalDocuments() error {
	return tc.sendGetNoAuth("/v1/legal")
}

func (tc *testContext) publicLegalDocumentsShouldBeAvailable() error {
	if err := tc.assertStatus(200); err != nil {
		return err
	}
	for _, path := range []string{
		"data.terms_of_service",
		"data.privacy_policy",
		"data.terms_of_service_version",
		"data.privacy_policy_version",
	} {
		if err := tc.assertJSONNotEmpty(path); err != nil {
			return err
		}
	}
	return nil
}

func (tc *testContext) userSignsUp(alias string) error {
	if err := tc.userHasSignedUp(alias); err != nil {
		return err
	}
	return tc.assertStatus(204)
}

func (tc *testContext) userSignsIn(alias string) error {
	if _, err := tc.doRequest("POST", "/v1/auth/sign-in", alias, nil); err != nil {
		return err
	}
	return tc.assertStatus(204)
}

func (tc *testContext) userShouldExistInDatabase(alias string) error {
	return tc.dbHasUserWithSubject(alias)
}

func (tc *testContext) userShouldHaveOneWorkspaceInDatabase(alias string) error {
	return tc.dbWorkspaceCountForSubject(1, alias)
}

func (tc *testContext) userOpensProfile(alias string) error {
	if _, err := tc.doRequest("GET", "/v1/users/profile", alias, nil); err != nil {
		return err
	}
	return tc.assertStatus(200)
}

func (tc *testContext) userShouldSeeDefaultProfileDetails(alias string) error {
	if err := tc.userOpensProfile(alias); err != nil {
		return err
	}
	if err := tc.assertJSONEqualsSubject("data.firebase_uid", alias); err != nil {
		return err
	}
	if err := tc.assertJSONEqualsEmail("data.email", alias); err != nil {
		return err
	}
	if err := tc.assertJSONEquals("data.is_deleted", "false"); err != nil {
		return err
	}
	if err := tc.assertJSONEquals("data.is_banned", "false"); err != nil {
		return err
	}
	return tc.assertJSONEquals("data.subscription.code", "freemium")
}

func (tc *testContext) userUpdatesProfileDetails(alias string) error {
	body := map[string]any{
		"nickname":      "berlin-builder",
		"name":          "Alex",
		"surname":       "Tester",
		"country_code":  "DE",
		"date_of_birth": "2000-01-15T00:00:00Z",
		"preferences": map[string]any{
			"platform_theme":    "dark",
			"platform_language": "en",
			"currency_code":     "EUR",
		},
	}
	if _, err := tc.doRequest("PATCH", "/v1/users", alias, body); err != nil {
		return err
	}
	return tc.assertStatus(204)
}

func (tc *testContext) userShouldSeeUpdatedProfileDetails(alias string) error {
	if err := tc.userOpensProfile(alias); err != nil {
		return err
	}
	for path, expected := range map[string]string{
		"data.nickname":                      "berlin-builder",
		"data.name":                          "Alex",
		"data.surname":                       "Tester",
		"data.country_code":                  "DE",
		"data.preferences.platform_theme":    "dark",
		"data.preferences.platform_language": "en",
		"data.preferences.currency_code":     "EUR",
	} {
		if err := tc.assertJSONEquals(path, expected); err != nil {
			return err
		}
	}
	if err := tc.dbUserHasNickname(alias, "berlin-builder"); err != nil {
		return err
	}
	return tc.dbUserHasCountryCode(alias, "DE")
}

func (tc *testContext) userRegistersPushToken(alias string) error {
	state := tc.userState(alias)
	state.pushToken = fmt.Sprintf("e2e-%s-push-%d", alias, time.Now().UnixNano())
	body := map[string]any{"push_token": state.pushToken}
	if _, err := tc.doRequest("POST", "/v1/fcm-token", alias, body); err != nil {
		return err
	}
	if err := tc.assertStatus(200); err != nil {
		return err
	}
	return tc.assertJSONEquals("data.success", "true")
}

func (tc *testContext) pushTokenShouldBeStored(alias string) error {
	token := tc.userState(alias).pushToken
	if token == "" {
		return fmt.Errorf("no push token recorded for user %q", alias)
	}
	return tc.dbHasPushToken(token, alias)
}

func (tc *testContext) userOpensLegalStatus(alias string) error {
	if _, err := tc.doRequest("GET", "/v1/users/legal", alias, nil); err != nil {
		return err
	}
	return tc.assertStatus(200)
}

func (tc *testContext) userShouldSeeCurrentLegalVersions(alias string) error {
	if err := tc.userOpensLegalStatus(alias); err != nil {
		return err
	}
	if err := tc.assertJSONNotEmpty("data.terms_of_service_version"); err != nil {
		return err
	}
	return tc.assertJSONNotEmpty("data.privacy_policy_version")
}

func (tc *testContext) userAcceptsCurrentLegalDocuments(alias string) error {
	if err := tc.userOpensLegalStatus(alias); err != nil {
		return err
	}
	body := map[string]any{
		"terms_of_service_version": gjson.GetBytes(tc.lastBody, "data.terms_of_service_version").String(),
		"privacy_policy_version":   gjson.GetBytes(tc.lastBody, "data.privacy_policy_version").String(),
	}
	if _, err := tc.doRequest("POST", "/v1/users/legal/accept", alias, body); err != nil {
		return err
	}
	return tc.assertStatus(204)
}

func (tc *testContext) userShouldShowAcceptedLegalDocuments(alias string) error {
	if err := tc.userOpensLegalStatus(alias); err != nil {
		return err
	}
	if err := tc.assertJSONEquals("data.has_agreed_with_terms", "true"); err != nil {
		return err
	}
	return tc.assertJSONEquals("data.has_agreed_with_privacy_policy", "true")
}

func (tc *testContext) userOpensWorkspaceList(alias string) error {
	if _, err := tc.doRequest("GET", "/v1/workspaces", alias, nil); err != nil {
		return err
	}
	if err := tc.assertStatus(200); err != nil {
		return err
	}
	return tc.captureDefaultWorkspace(alias)
}

func (tc *testContext) userShouldHaveSingleDefaultWorkspace(alias string) error {
	if err := tc.userOpensWorkspaceList(alias); err != nil {
		return err
	}
	if err := tc.assertJSONArrayLength("data", 1); err != nil {
		return err
	}
	return tc.assertJSONEquals("data.0.name", "My Swarm")
}

func (tc *testContext) userOpensDefaultWorkspace(alias string) error {
	if err := tc.ensureDefaultWorkspace(alias); err != nil {
		return err
	}
	state := tc.userState(alias)
	if _, err := tc.doRequest("GET", "/v1/workspaces/"+state.workspaceID, alias, nil); err != nil {
		return err
	}
	return tc.assertStatus(200)
}

func (tc *testContext) userShouldSeeRuntimeDetails(alias string) error {
	if err := tc.userOpensDefaultWorkspace(alias); err != nil {
		return err
	}
	if err := tc.assertJSONNotEmpty("data.runtime.status"); err != nil {
		return err
	}
	return tc.assertJSONNotEmpty("data.runtime.phase")
}

func (tc *testContext) userWaitsUntilAssistantIsReady(alias string) error {
	if err := tc.ensureDefaultWorkspace(alias); err != nil {
		return err
	}
	if err := tc.ensureConversationCatalog(alias); err != nil {
		return err
	}
	state := tc.userState(alias)
	if state.currentConversation == "" {
		state.currentConversation = state.swarmConversationID
	}
	deadline := time.Now().Add(tc.runtimeReadyTimeout())
	for {
		if _, err := tc.doRequest("GET", "/v1/workspaces/"+state.workspaceID, alias, nil); err != nil {
			return err
		}
		if err := tc.assertStatus(200); err != nil {
			return err
		}
		snapshot := runtimeSnapshotFromBody(tc.lastBody)
		if snapshot.ready() {
			if err := tc.sendWarmupMessage(alias); err != nil {
				return err
			}
			switch tc.lastStatus {
			case 200:
				return nil
			case 0, 500, 503:
				// The runtime reports ready but is still warming up internally.
			default:
				return fmt.Errorf("assistant warmup failed with unexpected status %d; body: %s", tc.lastStatus, abbreviatedBody(tc.lastBody))
			}
		}
		if snapshot.failed() {
			if snapshot.LastError != "" {
				return fmt.Errorf("workspace runtime entered failed state: %s", snapshot.LastError)
			}
			return fmt.Errorf("workspace runtime entered failed state: status=%q phase=%q", snapshot.Status, snapshot.Phase)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("workspace runtime did not become ready in %s; last status=%q phase=%q verified=%t error=%q", tc.runtimeReadyTimeout(), snapshot.Status, snapshot.Phase, snapshot.Verified, snapshot.LastError)
		}
		time.Sleep(tc.runtimePollInterval())
	}
}

func (tc *testContext) userShouldSeeWorkspaceRuntimeReady(alias string) error {
	if err := tc.userOpensDefaultWorkspace(alias); err != nil {
		return err
	}
	snapshot := runtimeSnapshotFromBody(tc.lastBody)
	if !snapshot.ready() {
		return fmt.Errorf("expected ready workspace runtime, got status=%q phase=%q verified=%t", snapshot.Status, snapshot.Phase, snapshot.Verified)
	}
	return nil
}

func (tc *testContext) userOpensAgents(alias string) error {
	if err := tc.ensureDefaultWorkspace(alias); err != nil {
		return err
	}
	state := tc.userState(alias)
	if _, err := tc.doRequest("GET", "/v1/workspaces/"+state.workspaceID+"/agents", alias, nil); err != nil {
		return err
	}
	if err := tc.assertStatus(200); err != nil {
		return err
	}
	return tc.captureAgents(alias)
}

func (tc *testContext) userShouldSeeDefaultAgents(alias string) error {
	if err := tc.userOpensAgents(alias); err != nil {
		return err
	}
	if err := tc.assertJSONArrayLength("data", 3); err != nil {
		return err
	}
	if err := tc.assertJSONArrayContainsItem("data", "slug", "manager"); err != nil {
		return err
	}
	if err := tc.assertJSONArrayContainsItem("data", "slug", "wally"); err != nil {
		return err
	}
	return tc.assertJSONArrayContainsItem("data", "slug", "eve")
}

func (tc *testContext) userOpensConversations(alias string) error {
	if err := tc.ensureDefaultWorkspace(alias); err != nil {
		return err
	}
	state := tc.userState(alias)
	if _, err := tc.doRequest("GET", "/v1/workspaces/"+state.workspaceID+"/conversations", alias, nil); err != nil {
		return err
	}
	if err := tc.assertStatus(200); err != nil {
		return err
	}
	return tc.captureConversations(alias)
}

func (tc *testContext) userShouldSeeDefaultConversations(alias string) error {
	if err := tc.userOpensConversations(alias); err != nil {
		return err
	}
	if err := tc.assertJSONArrayLength("data", 3); err != nil {
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
	if !gjson.GetBytes(tc.lastBody, "data.pagination.has_next").Exists() {
		return fmt.Errorf("data.pagination.has_next missing from response")
	}
	if !gjson.GetBytes(tc.lastBody, "data.pagination.has_prev").Exists() {
		return fmt.Errorf("data.pagination.has_prev missing from response")
	}
	return nil
}

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
	if err := tc.ensureAgentCatalog(alias); err != nil {
		return err
	}
	state := tc.userState(alias)
	normalizedRole := normalizeKey(role)
	agentID := state.agentIDsBySlug[normalizedRole]
	agentName := state.agentNamesBySlug[normalizedRole]
	if agentID == "" || agentName == "" {
		return fmt.Errorf("no agent found for role %q", role)
	}

	state.currentConversation = state.swarmConversationID
	mentionText := fmt.Sprintf("@%s %s", agentName, text)
	body := map[string]any{
		"local_id": tc.nextLocalID(alias, "mention"),
		"content": map[string]any{
			"type": "text",
			"text": mentionText,
		},
		"attachments": []any{},
		"mentions": []any{
			map[string]any{
				"agent_id":   agentID,
				"agent_name": agentName,
				"offset":     0,
				"length":     len("@" + agentName),
			},
		},
	}
	_, err := tc.doRequest("POST", "/v1/workspaces/"+state.workspaceID+"/conversations/"+state.currentConversation+"/messages", alias, body)
	return err
}

func (tc *testContext) assistantReplyShouldSucceed() error {
	return tc.assertStatus(200)
}

func (tc *testContext) assistantReplyShouldContainText() error {
	return tc.assertJSONNotEmpty("data.0.content.text")
}

func (tc *testContext) assistantReplyShouldComeFromAgent() error {
	if err := tc.assertJSONEquals("data.0.role", "agent"); err != nil {
		return err
	}
	if err := tc.assertJSONNotEmpty("data.0.agent.id"); err != nil {
		return err
	}
	if err := tc.assertJSONNotEmpty("data.0.agent.name"); err != nil {
		return err
	}
	return tc.assertJSONNotEmpty("data.0.agent.role")
}

func (tc *testContext) assistantReplyShouldComeFromSpecificAgent(role string) error {
	return tc.assertJSONEquals("data.0.agent.slug", normalizeKey(role))
}

func (tc *testContext) guestRequestsProfile() error {
	return tc.sendGetNoAuth("/v1/users/profile")
}

func (tc *testContext) userOpensMissingWorkspace(alias string) error {
	if _, err := tc.doRequest("GET", "/v1/workspaces/00000000-0000-0000-0000-000000000000", alias, nil); err != nil {
		return err
	}
	return nil
}

func (tc *testContext) userOpensMissingConversation(alias string) error {
	if err := tc.ensureDefaultWorkspace(alias); err != nil {
		return err
	}
	state := tc.userState(alias)
	if _, err := tc.doRequest("GET", "/v1/workspaces/"+state.workspaceID+"/conversations/00000000-0000-0000-0000-000000000000", alias, nil); err != nil {
		return err
	}
	return nil
}

func (tc *testContext) requestShouldBeUnauthorized() error {
	return tc.assertStatus(401)
}

func (tc *testContext) requestShouldBeRejectedAsInvalid() error {
	return tc.assertStatus(400)
}

func (tc *testContext) requestShouldBeRejectedAsNotFound() error {
	return tc.assertStatus(404)
}

func (tc *testContext) deletedAccountShouldNoLongerBehaveLikeActiveUser() error {
	switch tc.lastStatus {
	case 403:
		return nil
	case 200:
		return tc.assertJSONEquals("data.is_deleted", "true")
	default:
		return fmt.Errorf("expected deleted account response to be 200 or 403, got %d", tc.lastStatus)
	}
}

func (tc *testContext) usersShouldHaveDifferentDefaultWorkspaces(aliasA, aliasB string) error {
	if err := tc.ensureDefaultWorkspace(aliasA); err != nil {
		return err
	}
	if err := tc.ensureDefaultWorkspace(aliasB); err != nil {
		return err
	}
	a := tc.userState(aliasA).workspaceID
	b := tc.userState(aliasB).workspaceID
	if a == "" || b == "" {
		return fmt.Errorf("missing workspace IDs for %q or %q", aliasA, aliasB)
	}
	if a == b {
		return fmt.Errorf("expected different workspace IDs for %q and %q, both were %q", aliasA, aliasB, a)
	}
	return nil
}

func (tc *testContext) userOpensAnotherUsersWorkspace(alias, ownerAlias string) error {
	if err := tc.ensureDefaultWorkspace(ownerAlias); err != nil {
		return err
	}
	ownerState := tc.userState(ownerAlias)
	_, err := tc.doRequest("GET", "/v1/workspaces/"+ownerState.workspaceID, alias, nil)
	return err
}

func (tc *testContext) userOpensAnotherUsersAgents(alias, ownerAlias string) error {
	if err := tc.ensureDefaultWorkspace(ownerAlias); err != nil {
		return err
	}
	ownerState := tc.userState(ownerAlias)
	_, err := tc.doRequest("GET", "/v1/workspaces/"+ownerState.workspaceID+"/agents", alias, nil)
	return err
}

func (tc *testContext) userOpensAnotherUsersConversations(alias, ownerAlias string) error {
	if err := tc.ensureDefaultWorkspace(ownerAlias); err != nil {
		return err
	}
	ownerState := tc.userState(ownerAlias)
	_, err := tc.doRequest("GET", "/v1/workspaces/"+ownerState.workspaceID+"/conversations", alias, nil)
	return err
}

func (tc *testContext) deletingUserShouldNotAffectUser(deletedAlias, survivorAlias string) error {
	if err := tc.userDeletesTheirAccount(deletedAlias); err != nil {
		return err
	}
	if err := tc.userOpensProfile(survivorAlias); err != nil {
		return err
	}
	return tc.assertJSONEquals("data.is_deleted", "false")
}

func (tc *testContext) userDeletesTheirAccount(alias string) error {
	body := map[string]any{
		"reason":      "no longer needed",
		"description": "e2e account deletion flow",
	}
	if _, err := tc.doRequest("DELETE", "/v1/auth/delete", alias, body); err != nil {
		return err
	}
	return tc.assertStatus(204)
}

func (tc *testContext) userShouldBeMarkedAsDeletedInDatabase(alias string) error {
	if err := tc.dbUserHasDeletedAt(alias); err != nil {
		return err
	}
	return tc.dbUserIsDeleted(alias, "true")
}

func (tc *testContext) sendMessage(alias, text string) error {
	state := tc.userState(alias)
	if state.currentConversation == "" {
		if err := tc.userOpensSwarmConversation(alias); err != nil {
			return err
		}
	}
	body := map[string]any{
		"local_id": tc.nextLocalID(alias, "message"),
		"content": map[string]any{
			"type": "text",
			"text": text,
		},
		"attachments": []any{},
	}
	_, err := tc.doRequest("POST", "/v1/workspaces/"+state.workspaceID+"/conversations/"+state.currentConversation+"/messages", alias, body)
	return err
}

func (tc *testContext) sendWarmupMessage(alias string) error {
	state := tc.userState(alias)
	body := map[string]any{
		"local_id": tc.nextLocalID(alias, "warmup"),
		"content": map[string]any{
			"type": "text",
			"text": "Reply with the single word READY.",
		},
		"attachments": []any{},
	}
	_, err := tc.doRequest("POST", "/v1/workspaces/"+state.workspaceID+"/conversations/"+state.currentConversation+"/messages", alias, body)
	return err
}

func (tc *testContext) userState(alias string) *userJourneyState {
	state, ok := tc.state[alias]
	if ok {
		return state
	}
	state = &userJourneyState{
		agentIDsBySlug:       make(map[string]string),
		agentNamesBySlug:     make(map[string]string),
		conversationIDsByKey: make(map[string]string),
	}
	tc.state[alias] = state
	return state
}

func (tc *testContext) ensureDefaultWorkspace(alias string) error {
	state := tc.userState(alias)
	if state.workspaceID != "" {
		return nil
	}
	return tc.userOpensWorkspaceList(alias)
}

func (tc *testContext) ensureAgentCatalog(alias string) error {
	state := tc.userState(alias)
	if len(state.agentIDsBySlug) > 0 {
		return nil
	}
	return tc.userOpensAgents(alias)
}

func (tc *testContext) ensureConversationCatalog(alias string) error {
	state := tc.userState(alias)
	if state.swarmConversationID != "" || len(state.conversationIDsByKey) > 0 {
		return nil
	}
	return tc.userOpensConversations(alias)
}

func (tc *testContext) captureDefaultWorkspace(alias string) error {
	state := tc.userState(alias)
	workspaceID := gjson.GetBytes(tc.lastBody, "data.0.id").String()
	if workspaceID == "" {
		return fmt.Errorf("no default workspace found for %q", alias)
	}
	state.workspaceID = workspaceID
	state.workspaceName = gjson.GetBytes(tc.lastBody, "data.0.name").String()
	return nil
}

func (tc *testContext) captureAgents(alias string) error {
	state := tc.userState(alias)
	for _, item := range gjson.GetBytes(tc.lastBody, "data").Array() {
		slug := normalizeKey(item.Get("slug").String())
		if slug == "" {
			continue
		}
		state.agentIDsBySlug[slug] = item.Get("id").String()
		state.agentNamesBySlug[slug] = item.Get("name").String()
	}
	if len(state.agentIDsBySlug) == 0 {
		return fmt.Errorf("no agents returned for %q", alias)
	}
	return nil
}

func (tc *testContext) captureConversations(alias string) error {
	state := tc.userState(alias)
	for _, item := range gjson.GetBytes(tc.lastBody, "data").Array() {
		convID := item.Get("id").String()
		if convID == "" {
			continue
		}
		convType := normalizeKey(item.Get("type").String())
		titleKey := normalizeKey(item.Get("title").String())
		agentSlug := normalizeKey(item.Get("agent.slug").String())
		if convType == "swarm" {
			state.swarmConversationID = convID
			state.conversationIDsByKey["swarm"] = convID
		}
		if titleKey != "" {
			state.conversationIDsByKey[titleKey] = convID
		}
		if agentSlug != "" {
			state.conversationIDsByKey[agentSlug] = convID
		}
	}
	if state.swarmConversationID == "" {
		return fmt.Errorf("no swarm conversation returned for %q", alias)
	}
	return nil
}

func (tc *testContext) nextLocalID(alias, kind string) string {
	return fmt.Sprintf("e2e-%s-%s-%d", alias, kind, time.Now().UnixNano())
}

func (tc *testContext) runtimeReadyTimeout() time.Duration {
	if tc.cfg != nil && tc.cfg.RuntimeReadyTimeout > 0 {
		return tc.cfg.RuntimeReadyTimeout
	}
	return defaultRuntimeReadyTimeout
}

func (tc *testContext) runtimePollInterval() time.Duration {
	if tc.cfg != nil && tc.cfg.RuntimePollInterval > 0 {
		return tc.cfg.RuntimePollInterval
	}
	return defaultRuntimePollInterval
}

func runtimeSnapshotFromBody(body []byte) runtimeSnapshot {
	return runtimeSnapshot{
		Status:    gjson.GetBytes(body, "data.runtime.status").String(),
		Phase:     gjson.GetBytes(body, "data.runtime.phase").String(),
		Verified:  gjson.GetBytes(body, "data.runtime.verified").Bool(),
		LastError: gjson.GetBytes(body, "data.runtime.last_error").String(),
	}
}

func (snapshot runtimeSnapshot) ready() bool {
	return snapshot.Verified && (strings.EqualFold(snapshot.Status, "ready") || strings.EqualFold(snapshot.Phase, "ready"))
}

func (snapshot runtimeSnapshot) failed() bool {
	return strings.EqualFold(snapshot.Status, "failed") || strings.EqualFold(snapshot.Phase, "error")
}

func normalizeKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func abbreviatedBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > 200 {
		return text[:200]
	}
	return text
}

func (tc *testContext) userOpensIntegrationsCatalog(alias string) error {
	if _, err := tc.doRequest("GET", "/v1/integrations", alias, nil); err != nil {
		return err
	}
	return tc.assertStatus(200)
}

func (tc *testContext) userShouldSeeToolCategories(alias string) error {
	if err := tc.userOpensIntegrationsCatalog(alias); err != nil {
		return err
	}
	categories := gjson.GetBytes(tc.lastBody, "data.categories")
	if !categories.IsArray() || len(categories.Array()) == 0 {
		return fmt.Errorf("expected non-empty categories array")
	}
	first := categories.Array()[0]
	for _, field := range []string{"id", "name", "image_url"} {
		if first.Get(field).String() == "" {
			return fmt.Errorf("category missing required field %q", field)
		}
	}
	return nil
}

func (tc *testContext) userShouldSeeToolsInCatalog(alias string) error {
	if err := tc.userOpensIntegrationsCatalog(alias); err != nil {
		return err
	}
	items := gjson.GetBytes(tc.lastBody, "data.items")
	if !items.IsArray() || len(items.Array()) == 0 {
		return fmt.Errorf("expected non-empty items array")
	}
	// Verify at least one tool item exists.
	for _, item := range items.Array() {
		if item.Get("type").String() == "tool" {
			// Verify required fields on the first tool found.
			for _, field := range []string{"name", "description", "icon_url", "category_id"} {
				if item.Get(field).String() == "" {
					return fmt.Errorf("tool item missing required field %q", field)
				}
			}
			return nil
		}
	}
	return fmt.Errorf("no items with type=tool found")
}

func (tc *testContext) userShouldSeeIntegrationAppsInCatalog(alias string) error {
	if err := tc.userOpensIntegrationsCatalog(alias); err != nil {
		return err
	}
	items := gjson.GetBytes(tc.lastBody, "data.items")
	if !items.IsArray() || len(items.Array()) == 0 {
		return fmt.Errorf("expected non-empty items array")
	}
	// Verify at least one app item exists.
	for _, item := range items.Array() {
		if item.Get("type").String() == "app" {
			for _, field := range []string{"name", "description", "icon_url", "provider", "category_id"} {
				if item.Get(field).String() == "" {
					return fmt.Errorf("app item missing required field %q", field)
				}
			}
			return nil
		}
	}
	return fmt.Errorf("no items with type=app found")
}
