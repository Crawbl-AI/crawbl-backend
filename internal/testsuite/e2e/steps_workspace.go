package e2e

import (
	"context"
	"fmt"
	"net/http"

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
	if _, err := tc.doRequest("GET", workspacesPath+state.workspaceID, alias, nil); err != nil {
		return err
	}
	return tc.assertStatus(http.StatusOK)
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
	ctx, cancel := context.WithTimeout(context.Background(), tc.runtimeReadyTimeout())
	defer cancel()

	return pollUntil(ctx, func() error {
		return tc.pollAssistantReady(alias, state.workspaceID)
	})
}

// pollAssistantReady is a single iteration of the assistant-ready poll loop.
// It fetches the workspace snapshot, issues the warmup message once the
// runtime reports ready, and surfaces a descriptive error while warming.
func (tc *testContext) pollAssistantReady(alias, workspaceID string) error {
	if _, err := tc.doRequest("GET", workspacesPath+workspaceID, alias, nil); err != nil {
		return err
	}
	if err := tc.assertStatus(http.StatusOK); err != nil {
		return err
	}
	snapshot := runtimeSnapshotFromBody(tc.lastBody)
	if snapshot.ready() {
		return tc.issueWarmupAndCheck(alias)
	}
	if snapshot.failed() {
		return runtimeFailedError(snapshot)
	}
	return fmt.Errorf("runtime not ready: status=%q phase=%q verified=%t error=%q", snapshot.Status, snapshot.Phase, snapshot.Verified, snapshot.LastError)
}

// issueWarmupAndCheck sends the warmup message and translates the HTTP
// response code into a "retry while warming" error or nil for success.
func (tc *testContext) issueWarmupAndCheck(alias string) error {
	// Cold-start can exceed the per-request HTTP client timeout; treat any
	// transport-level error as "retry and keep polling the runtime" rather
	// than a hard failure. The context deadline still bounds the loop.
	if err := tc.sendWarmupMessage(alias); err != nil {
		return err
	}
	switch tc.lastStatus {
	case http.StatusOK, http.StatusCreated:
		// 201 Created is the current contract for POST
		// /v1/workspaces/{id}/conversations/{id}/messages — the handler
		// persists the user message and returns immediately, leaving the
		// assistant reply to stream over Socket.IO.
		return nil
	case 0, http.StatusInternalServerError, http.StatusServiceUnavailable:
		// Runtime reports ready but still warming up internally.
		return fmt.Errorf("runtime warming up (status=%d)", tc.lastStatus)
	default:
		return fmt.Errorf("assistant warmup failed with unexpected status %d; body: %s", tc.lastStatus, abbreviatedBody(tc.lastBody))
	}
}

func runtimeFailedError(snapshot runtimeSnapshot) error {
	if snapshot.LastError != "" {
		return fmt.Errorf("workspace runtime entered failed state: %s", snapshot.LastError)
	}
	return fmt.Errorf("workspace runtime entered failed state: status=%q phase=%q", snapshot.Status, snapshot.Phase)
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
	path := workspacesPath + state.workspaceID + conversationsPath + "00000000-0000-0000-0000-000000000000"
	_, err := tc.doRequest("GET", path, alias, nil)
	return err
}
