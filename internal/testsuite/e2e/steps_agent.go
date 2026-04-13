package e2e

import (
	"fmt"

	"github.com/cucumber/godog"
	"github.com/tidwall/gjson"
)

func registerAgentSteps(sc *godog.ScenarioContext, tc *testContext) {
	sc.Step(`^user "([^"]*)" opens the details for agent "([^"]*)"$`, tc.userOpensAgentDetails)
	sc.Step(`^user "([^"]*)" opens the agent "([^"]*)"$`, tc.userOpensAgent)
	sc.Step(`^user "([^"]*)" opens the history for agent "([^"]*)"$`, tc.userOpensAgentHistory)
	sc.Step(`^user "([^"]*)" opens the settings for agent "([^"]*)"$`, tc.userOpensAgentSettings)
	sc.Step(`^user "([^"]*)" opens the tools for agent "([^"]*)"$`, tc.userOpensAgentTools)
	sc.Step(`^user "([^"]*)" opens the memories for agent "([^"]*)"$`, tc.userOpensAgentMemories)
	sc.Step(`^user "([^"]*)" saves a memory with key "([^"]*)" and content "([^"]*)" for agent "([^"]*)"$`, tc.userSavesAgentMemory)
	sc.Step(`^user "([^"]*)" has saved a memory with key "([^"]*)" and content "([^"]*)" for agent "([^"]*)"$`, tc.userSavesAgentMemory)
	sc.Step(`^user "([^"]*)" deletes the memory with key "([^"]*)" for agent "([^"]*)"$`, tc.userDeletesAgentMemory)
	sc.Step(`^the memory with key "([^"]*)" should no longer exist for agent "([^"]*)"$`, tc.memoryShouldNotExist)
	sc.Step(`^the memory with key "([^"]*)" should exist for agent "([^"]*)"$`, tc.memoryShouldExist)
}

func (tc *testContext) userOpensAgentDetails(alias, slug string) error {
	id, err := tc.agentIDForSlug(alias, slug)
	if err != nil {
		return err
	}
	_, err = tc.doRequest("GET", agentsPath+id+"/details", alias, nil)
	return err
}

// userOpensAgent hits the plain GET /v1/agents/{id} endpoint (no
// /details suffix). The two endpoints exist as separate handlers so
// the test suite covers both independently.
func (tc *testContext) userOpensAgent(alias, slug string) error {
	id, err := tc.agentIDForSlug(alias, slug)
	if err != nil {
		return err
	}
	_, err = tc.doRequest("GET", agentsPath+id, alias, nil)
	return err
}

func (tc *testContext) userOpensAgentHistory(alias, slug string) error {
	id, err := tc.agentIDForSlug(alias, slug)
	if err != nil {
		return err
	}
	_, err = tc.doRequest("GET", agentsPath+id+"/history", alias, nil)
	return err
}

func (tc *testContext) userOpensAgentSettings(alias, slug string) error {
	id, err := tc.agentIDForSlug(alias, slug)
	if err != nil {
		return err
	}
	_, err = tc.doRequest("GET", agentsPath+id+"/settings", alias, nil)
	return err
}

func (tc *testContext) userOpensAgentTools(alias, slug string) error {
	id, err := tc.agentIDForSlug(alias, slug)
	if err != nil {
		return err
	}
	_, err = tc.doRequest("GET", agentsPath+id+"/tools", alias, nil)
	return err
}

func (tc *testContext) userOpensAgentMemories(alias, slug string) error {
	id, err := tc.agentIDForSlug(alias, slug)
	if err != nil {
		return err
	}
	_, err = tc.doRequest("GET", agentsPath+id+memoriesPath, alias, nil)
	return err
}

func (tc *testContext) userSavesAgentMemory(alias, key, content, slug string) error {
	id, err := tc.agentIDForSlug(alias, slug)
	if err != nil {
		return err
	}
	body := map[string]any{"key": key, "content": content}
	_, err = tc.doRequest("POST", agentsPath+id+memoriesPath, alias, body)
	return err
}

func (tc *testContext) userDeletesAgentMemory(alias, key, slug string) error {
	id, err := tc.agentIDForSlug(alias, slug)
	if err != nil {
		return err
	}
	_, err = tc.doRequest("DELETE", agentsPath+id+memoriesPath+"/"+key, alias, map[string]any{})
	return err
}

// memoryShouldNotExist lists the agent's memories and verifies no entry
// has the given key. Uses the "primary" user alias implicitly via the
// most-recent user state.
func (tc *testContext) memoryShouldNotExist(key, slug string) error {
	return tc.assertMemoryKeyExists("primary", key, slug, false)
}

// memoryShouldExist is the positive counterpart of memoryShouldNotExist.
func (tc *testContext) memoryShouldExist(key, slug string) error {
	return tc.assertMemoryKeyExists("primary", key, slug, true)
}

func (tc *testContext) assertMemoryKeyExists(alias, key, slug string, shouldExist bool) error {
	id, err := tc.agentIDForSlug(alias, slug)
	if err != nil {
		return err
	}
	if _, err := tc.doRequest("GET", agentsPath+id+memoriesPath, alias, nil); err != nil {
		return err
	}
	entries := gjson.GetBytes(tc.lastBody, "data").Array()
	for _, e := range entries {
		if e.Get("key").String() == key {
			if shouldExist {
				return nil
			}
			return fmt.Errorf("memory key %q still exists for agent %q, expected removal", key, slug)
		}
	}
	if shouldExist {
		return fmt.Errorf("memory key %q not found for agent %q, expected it to exist", key, slug)
	}
	return nil
}
