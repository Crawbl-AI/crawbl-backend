package e2e

import (
	"fmt"
	"time"

	"github.com/cucumber/godog"
)

func registerUserSteps(sc *godog.ScenarioContext, tc *testContext) {
	sc.Step(`^a new test user "([^"]*)"$`, tc.aNewTestUser)
	sc.Step(`^user "([^"]*)" has signed up$`, tc.userHasSignedUp)
	sc.Step(`^user "([^"]*)" has a workspace saved as "([^"]*)"$`, tc.userHasWorkspaceSaved)
	sc.Step(`^user "([^"]*)" has a conversation saved as "([^"]*)"$`, tc.userHasConversationSaved)
	sc.Step(`^user "([^"]*)" has deleted their account$`, tc.userHasDeletedAccount)
}

func (tc *testContext) aNewTestUser(alias string) error {
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tc.users[alias] = &testUser{
		alias:   alias,
		subject: fmt.Sprintf("e2e-%s-%s", alias, suffix),
		email:   fmt.Sprintf("e2e-%s-%s@crawbl.test", alias, suffix),
		name:    fmt.Sprintf("E2E %s", alias),
	}
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

// findWorkspaceID looks for a saved workspace_id, or fetches one.
func (tc *testContext) findWorkspaceID(alias string) string {
	for k, v := range tc.saved {
		if v != "" && (k == "workspace_id" || k == alias+"_workspace") {
			return v
		}
	}
	// Try fetching.
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
