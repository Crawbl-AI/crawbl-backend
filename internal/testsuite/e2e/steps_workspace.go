package e2e

import (
	"fmt"
	"time"

	"github.com/cucumber/godog"
)

func registerWorkspaceSteps(sc *godog.ScenarioContext, tc *testContext) {
	sc.Step(`^user "([^"]*)" opens their workspace list$`, tc.userOpensWorkspaceList)
	sc.Step(`^user "([^"]*)" should have a single default workspace$`, tc.userShouldHaveSingleDefaultWorkspace)
	sc.Step(`^user "([^"]*)" opens their default workspace$`, tc.userOpensDefaultWorkspace)
	sc.Step(`^user "([^"]*)" should see runtime details for their default workspace$`, tc.userShouldSeeRuntimeDetails)
	sc.Step(`^user "([^"]*)" waits until their assistant is ready$`, tc.userWaitsUntilAssistantIsReady)
	sc.Step(`^user "([^"]*)" should see their workspace runtime as ready$`, tc.userShouldSeeWorkspaceRuntimeReady)
}

func (tc *testContext) userOpensWorkspaceList(alias string) error {
	return tc.fetchWorkspaceList(alias)
}

func (tc *testContext) userShouldHaveSingleDefaultWorkspace(alias string) error {
	if err := tc.userOpensWorkspaceList(alias); err != nil {
		return err
	}
	return tc.assertJSONArrayLength("data", 1)
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
				// Runtime reports ready but still warming up internally.
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
			return fmt.Errorf("workspace runtime did not become ready in %s; last status=%q phase=%q verified=%t error=%q",
				tc.runtimeReadyTimeout(), snapshot.Status, snapshot.Phase, snapshot.Verified, snapshot.LastError)
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

// userOpensMissingWorkspace tries to open a non-existent workspace.
func (tc *testContext) userOpensMissingWorkspace(alias string) error {
	_, err := tc.doRequest("GET", "/v1/workspaces/00000000-0000-0000-0000-000000000000", alias, nil)
	return err
}

// userOpensMissingConversation tries to open a non-existent conversation.
func (tc *testContext) userOpensMissingConversation(alias string) error {
	if err := tc.ensureDefaultWorkspace(alias); err != nil {
		return err
	}
	state := tc.userState(alias)
	path := "/v1/workspaces/" + state.workspaceID + "/conversations/00000000-0000-0000-0000-000000000000"
	_, err := tc.doRequest("GET", path, alias, nil)
	return err
}

