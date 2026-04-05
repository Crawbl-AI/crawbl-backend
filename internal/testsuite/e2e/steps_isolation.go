package e2e

import (
	"fmt"

	"github.com/cucumber/godog"
)

func registerIsolationSteps(sc *godog.ScenarioContext, tc *testContext) {
	sc.Step(`^users "([^"]*)" and "([^"]*)" should have different default workspaces$`, tc.usersShouldHaveDifferentDefaultWorkspaces)
	sc.Step(`^user "([^"]*)" opens user "([^"]*)"'s default workspace$`, tc.userOpensAnotherUsersWorkspace)
	sc.Step(`^user "([^"]*)" opens user "([^"]*)"'s agents$`, tc.userOpensAnotherUsersAgents)
	sc.Step(`^user "([^"]*)" opens user "([^"]*)"'s conversations$`, tc.userOpensAnotherUsersConversations)
	sc.Step(`^deleting user "([^"]*)" should not affect user "([^"]*)"$`, tc.deletingUserShouldNotAffectUser)
}

func (tc *testContext) usersShouldHaveDifferentDefaultWorkspaces(aliasA, aliasB string) error {
	if err := tc.ensureDefaultWorkspace(aliasA); err != nil {
		return err
	}
	if err := tc.ensureDefaultWorkspace(aliasB); err != nil {
		return err
	}
	wsA := tc.userState(aliasA).workspaceID
	wsB := tc.userState(aliasB).workspaceID
	if wsA == wsB {
		return fmt.Errorf("users %q and %q share the same workspace %q", aliasA, aliasB, wsA)
	}
	return nil
}

func (tc *testContext) userOpensAnotherUsersWorkspace(alias, ownerAlias string) error {
	if err := tc.ensureDefaultWorkspace(ownerAlias); err != nil {
		return err
	}
	ownerWS := tc.userState(ownerAlias).workspaceID
	_, err := tc.doRequest("GET", "/v1/workspaces/"+ownerWS, alias, nil)
	return err
}

func (tc *testContext) userOpensAnotherUsersAgents(alias, ownerAlias string) error {
	if err := tc.ensureDefaultWorkspace(ownerAlias); err != nil {
		return err
	}
	ownerWS := tc.userState(ownerAlias).workspaceID
	_, err := tc.doRequest("GET", "/v1/workspaces/"+ownerWS+"/agents", alias, nil)
	return err
}

func (tc *testContext) userOpensAnotherUsersConversations(alias, ownerAlias string) error {
	if err := tc.ensureDefaultWorkspace(ownerAlias); err != nil {
		return err
	}
	ownerWS := tc.userState(ownerAlias).workspaceID
	_, err := tc.doRequest("GET", "/v1/workspaces/"+ownerWS+"/conversations", alias, nil)
	return err
}

func (tc *testContext) deletingUserShouldNotAffectUser(deletedAlias, survivorAlias string) error {
	if err := tc.userDeletesTheirAccount(deletedAlias); err != nil {
		return err
	}
	if err := tc.userOpensProfile(survivorAlias); err != nil {
		return err
	}
	return tc.assertStatus(200)
}
