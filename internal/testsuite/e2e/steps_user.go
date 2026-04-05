// Package e2e — user lifecycle step definitions.
// All 3 test users (primary, frank, grace) are pre-created at suite level.
// No dynamic user creation — prevents runtime instance explosion.
package e2e

import (
	"fmt"

	"github.com/cucumber/godog"
)

func registerUserSteps(sc *godog.ScenarioContext, tc *testContext) {
	sc.Step(`^the primary test user$`, tc.usePrimaryUser)
	sc.Step(`^the primary test user has signed up$`, tc.primaryHasSignedUp)
	sc.Step(`^an extra test user "([^"]*)"$`, tc.useExtraUser)
	sc.Step(`^user "([^"]*)" has signed up$`, tc.userHasSignedUp)
	sc.Step(`^user "([^"]*)" has a workspace saved as "([^"]*)"$`, tc.userHasWorkspaceSaved)
	sc.Step(`^user "([^"]*)" has a conversation saved as "([^"]*)"$`, tc.userHasConversationSaved)
	sc.Step(`^user "([^"]*)" has deleted their account$`, tc.userHasDeletedAccount)
}

func (tc *testContext) usePrimaryUser() error {
	if _, ok := tc.users["primary"]; !ok {
		return fmt.Errorf("primary user not initialized")
	}
	return nil
}

func (tc *testContext) primaryHasSignedUp() error {
	return tc.userHasSignedUp("primary")
}

// useExtraUser references a pre-created suite-level user (frank or grace).
// No new users are created — this just verifies the alias exists.
func (tc *testContext) useExtraUser(alias string) error {
	if _, ok := tc.users[alias]; !ok {
		return fmt.Errorf("unknown user %q — only primary, frank, grace are available", alias)
	}
	return nil
}

func (tc *testContext) userHasSignedUp(alias string) error {
	user := tc.users[alias]
	if user == nil {
		return fmt.Errorf("unknown user %q", alias)
	}
	// Sign-up is idempotent — safe to call multiple times.
	_, err := tc.doRequest("POST", "/v1/auth/sign-up", alias, nil)
	if err != nil {
		return err
	}
	user.signedUp = true
	return nil
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
