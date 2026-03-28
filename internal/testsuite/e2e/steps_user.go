// Package e2e — user lifecycle step definitions.
// Manages test user creation, sign-up, and workspace/conversation state.
// The "primary" user is shared across all scenarios (created once at suite level).
// Additional users ("extra") are created per-scenario and cleaned up after.
package e2e

import (
	"fmt"
	"time"

	"github.com/cucumber/godog"
)

// MaxExtraUsers caps how many additional users a scenario can create
// to prevent UserSwarm explosion in the cluster.
const MaxExtraUsers = 2

func registerUserSteps(sc *godog.ScenarioContext, tc *testContext) {
	sc.Step(`^the primary test user$`, tc.usePrimaryUser)
	sc.Step(`^the primary test user has signed up$`, tc.primaryHasSignedUp)
	sc.Step(`^an extra test user "([^"]*)"$`, tc.createExtraUser)
	sc.Step(`^user "([^"]*)" has signed up$`, tc.userHasSignedUp)
	sc.Step(`^user "([^"]*)" has a workspace saved as "([^"]*)"$`, tc.userHasWorkspaceSaved)
	sc.Step(`^user "([^"]*)" has a conversation saved as "([^"]*)"$`, tc.userHasConversationSaved)
	sc.Step(`^user "([^"]*)" has deleted their account$`, tc.userHasDeletedAccount)
}

// usePrimaryUser makes the primary user available in the scenario (no-op, already loaded).
func (tc *testContext) usePrimaryUser() error {
	if _, ok := tc.users["primary"]; !ok {
		return fmt.Errorf("primary user not initialized")
	}
	return nil
}

// primaryHasSignedUp signs up the primary user (idempotent).
func (tc *testContext) primaryHasSignedUp() error {
	_, err := tc.doRequest("POST", "/v1/auth/sign-up", "primary", nil)
	return err
}

// createExtraUser creates an additional test user for multi-user scenarios.
// Capped at MaxExtraUsers to prevent UserSwarm explosion.
func (tc *testContext) createExtraUser(alias string) error {
	if len(tc.extraUsers) >= MaxExtraUsers {
		return fmt.Errorf("max %d extra users per scenario (preventing UserSwarm explosion)", MaxExtraUsers)
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tc.users[alias] = &testUser{
		alias:   alias,
		subject: fmt.Sprintf("e2e-%s-%s", alias, suffix),
		email:   fmt.Sprintf("e2e-%s-%s@crawbl.test", alias, suffix),
		name:    fmt.Sprintf("E2E %s", alias),
	}
	tc.extraUsers = append(tc.extraUsers, alias)
	return nil
}

func (tc *testContext) userHasSignedUp(alias string) error {
	_, err := tc.doRequest("POST", "/v1/auth/sign-up", alias, nil)
	return err
}

func (tc *testContext) userHasWorkspaceSaved(alias, key string) error {
	resp, err := tc.doRequest("GET", "/v1/workspaces", alias, nil)
	if err != nil {
		return err
	}
	id := gjsonGet(resp, "data.0.id")
	if id == "" {
		return fmt.Errorf("no workspace found for user %q", alias)
	}
	tc.saved[key] = id
	return nil
}

func (tc *testContext) userHasConversationSaved(alias, key string) error {
	wsID := tc.findWorkspaceID(alias)
	if wsID == "" {
		return fmt.Errorf("no workspace_id in state for user %q", alias)
	}
	resp, err := tc.doRequest("GET", "/v1/workspaces/"+wsID+"/conversations", alias, nil)
	if err != nil {
		return err
	}
	id := gjsonGet(resp, "data.0.id")
	if id == "" {
		return fmt.Errorf("no conversation found for user %q", alias)
	}
	tc.saved[key] = id
	return nil
}

func (tc *testContext) userHasDeletedAccount(alias string) error {
	body := map[string]any{
		"reason":      "e2e-test",
		"description": "setup for deletion test",
	}
	_, err := tc.doRequest("DELETE", "/v1/auth/delete", alias, body)
	return err
}

// findWorkspaceID retrieves or fetches the workspace ID for a user.
func (tc *testContext) findWorkspaceID(alias string) string {
	for k, v := range tc.saved {
		if v != "" && (k == "workspace_id" || k == alias+"_workspace") {
			return v
		}
	}
	resp, err := tc.doRequest("GET", "/v1/workspaces", alias, nil)
	if err != nil {
		return ""
	}
	id := gjsonGet(resp, "data.0.id")
	if id != "" {
		tc.saved["workspace_id"] = id
	}
	return id
}
