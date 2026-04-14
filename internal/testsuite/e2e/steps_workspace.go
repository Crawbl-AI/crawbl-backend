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
	if _, err := tc.doRequest("GET", pathWorkspaces+state.workspaceID, alias, nil); err != nil {
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
		if _, err := tc.doRequest("GET", pathWorkspaces+state.workspaceID, alias, nil); err != nil {
			return err
		}
		if err := tc.assertStatus(http.StatusOK); err != nil {
			return err
		}
		snapshot := runtimeSnapshotFromBody(tc.lastBody)
		if snapshot.ready() {
			return tc.checkWarmupStatus(alias)
		}
		if snapshot.failed() {
			return snapshotFailedError(snapshot)
		}
		return fmt.Errorf("runtime not ready: status=%q phase=%q verified=%t error=%q", snapshot.Status, snapshot.Phase, snapshot.Verified, snapshot.LastError)
	})
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

// checkWarmupStatus sends a warmup message and inspects the HTTP status to
// determine whether the runtime is truly ready for traffic.
func (tc *testContext) checkWarmupStatus(alias string) error {
	if err := tc.sendWarmupMessage(alias); err != nil {
		// Cold-start can exceed the per-request HTTP client timeout;
		// treat transport-level errors as "retry" rather than hard failure.
		return err
	}
	switch tc.lastStatus {
	case http.StatusOK, http.StatusCreated:
		return nil
	case 0, 500, http.StatusServiceUnavailable:
		return fmt.Errorf("runtime warming up (status=%d)", tc.lastStatus)
	default:
		return fmt.Errorf("assistant warmup failed with unexpected status %d; body: %s", tc.lastStatus, abbreviatedBody(tc.lastBody))
	}
}

// snapshotFailedError converts a failed runtime snapshot into a descriptive error.
func snapshotFailedError(snapshot runtimeSnapshot) error {
	if snapshot.LastError != "" {
		return fmt.Errorf("workspace runtime entered failed state: %s", snapshot.LastError)
	}
	return fmt.Errorf("workspace runtime entered failed state: status=%q phase=%q", snapshot.Status, snapshot.Phase)
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
	path := pathWorkspaces + state.workspaceID + "/conversations/00000000-0000-0000-0000-000000000000"
	_, err := tc.doRequest("GET", path, alias, nil)
	return err
}
