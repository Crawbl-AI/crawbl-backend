package e2e

import "github.com/cucumber/godog"

func registerAgentSteps(sc *godog.ScenarioContext, tc *testContext) {
	sc.Step(`^user "([^"]*)" opens the details for agent "([^"]*)"$`, tc.userOpensAgentDetails)
	sc.Step(`^user "([^"]*)" opens the history for agent "([^"]*)"$`, tc.userOpensAgentHistory)
	sc.Step(`^user "([^"]*)" opens the settings for agent "([^"]*)"$`, tc.userOpensAgentSettings)
	sc.Step(`^user "([^"]*)" opens the tools for agent "([^"]*)"$`, tc.userOpensAgentTools)
	sc.Step(`^user "([^"]*)" opens the memories for agent "([^"]*)"$`, tc.userOpensAgentMemories)
	sc.Step(`^user "([^"]*)" saves a memory with key "([^"]*)" and content "([^"]*)" for agent "([^"]*)"$`, tc.userSavesAgentMemory)
	sc.Step(`^user "([^"]*)" deletes the memory with key "([^"]*)" for agent "([^"]*)"$`, tc.userDeletesAgentMemory)
}

func (tc *testContext) userOpensAgentDetails(alias, slug string) error {
	id, err := tc.agentIDForSlug(alias, slug)
	if err != nil {
		return err
	}
	_, err = tc.doRequest("GET", "/v1/agents/"+id+"/details", alias, nil)
	return err
}

func (tc *testContext) userOpensAgentHistory(alias, slug string) error {
	id, err := tc.agentIDForSlug(alias, slug)
	if err != nil {
		return err
	}
	_, err = tc.doRequest("GET", "/v1/agents/"+id+"/history", alias, nil)
	return err
}

func (tc *testContext) userOpensAgentSettings(alias, slug string) error {
	id, err := tc.agentIDForSlug(alias, slug)
	if err != nil {
		return err
	}
	_, err = tc.doRequest("GET", "/v1/agents/"+id+"/settings", alias, nil)
	return err
}

func (tc *testContext) userOpensAgentTools(alias, slug string) error {
	id, err := tc.agentIDForSlug(alias, slug)
	if err != nil {
		return err
	}
	_, err = tc.doRequest("GET", "/v1/agents/"+id+"/tools", alias, nil)
	return err
}

func (tc *testContext) userOpensAgentMemories(alias, slug string) error {
	id, err := tc.agentIDForSlug(alias, slug)
	if err != nil {
		return err
	}
	_, err = tc.doRequest("GET", "/v1/agents/"+id+"/memories", alias, nil)
	return err
}

func (tc *testContext) userSavesAgentMemory(alias, key, content, slug string) error {
	id, err := tc.agentIDForSlug(alias, slug)
	if err != nil {
		return err
	}
	body := map[string]any{"key": key, "content": content}
	_, err = tc.doRequest("POST", "/v1/agents/"+id+"/memories", alias, body)
	return err
}

func (tc *testContext) userDeletesAgentMemory(alias, key, slug string) error {
	id, err := tc.agentIDForSlug(alias, slug)
	if err != nil {
		return err
	}
	_, err = tc.doRequest("DELETE", "/v1/agents/"+id+"/memories/"+key, alias, map[string]any{})
	return err
}
