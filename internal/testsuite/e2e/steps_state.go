package e2e

import (
	"fmt"

	"github.com/cucumber/godog"
	"github.com/tidwall/gjson"
)

func registerStateSteps(sc *godog.ScenarioContext, tc *testContext) {
	sc.Step(`^I save "([^"]*)" as "([^"]*)"$`, tc.saveJSONValue)
	sc.Step(`^I save the first item in "([^"]*)" where "([^"]*)" equals "([^"]*)" field "([^"]*)" as "([^"]*)"$`, tc.saveFirstMatchingItem)
	sc.Step(`^I save the first item in "([^"]*)" where "([^"]*)" equals "([^"]*)" field "([^"]*)" as "([^"]*)" from conversations$`, tc.saveFromConversations)
	sc.Step(`^I save response array length "([^"]*)" as "([^"]*)"$`, tc.saveArrayLength)
}

func (tc *testContext) saveJSONValue(jsonPath, key string) error {
	val := gjson.GetBytes(tc.lastBody, jsonPath).String()
	tc.saved[key] = val
	return nil
}

// saveFirstMatchingItem finds the first array item where matchField==matchValue
// and saves the given field as the key.
func (tc *testContext) saveFirstMatchingItem(arrayPath, matchField, matchValue, saveField, key string) error {
	arr := gjson.GetBytes(tc.lastBody, arrayPath)
	if !arr.IsArray() {
		return fmt.Errorf("JSON %s is not an array", arrayPath)
	}
	for _, item := range arr.Array() {
		if item.Get(matchField).String() == matchValue {
			tc.saved[key] = item.Get(saveField).String()
			return nil
		}
	}
	return fmt.Errorf("no item in %s where %s=%q", arrayPath, matchField, matchValue)
}

// saveFromConversations fetches conversations for the workspace and saves a matching field.
// Used when the current response body is not the conversations list.
//
// TODO(a-babayev): the user alias is hardcoded to "primary"; if a future step needs to fetch
// conversations as frank or grace, extract the alias into the Gherkin step pattern and add
// an aliasName parameter here.
func (tc *testContext) saveFromConversations(arrayPath, matchField, matchValue, saveField, key string) error {
	wsID := tc.saved["workspace_id"]
	if wsID == "" {
		return fmt.Errorf("no workspace_id saved — fetch workspaces first")
	}
	// Fetch conversations, save into lastBody temporarily.
	resp, err := tc.doRequest("GET", workspacesPath+wsID+"/conversations", "primary", nil)
	if err != nil {
		return fmt.Errorf("fetch conversations: %w", err)
	}
	arr := gjson.Get(resp, arrayPath)
	if !arr.IsArray() {
		return fmt.Errorf("conversations %s is not an array", arrayPath)
	}
	for _, item := range arr.Array() {
		if item.Get(matchField).String() == matchValue {
			tc.saved[key] = item.Get(saveField).String()
			return nil
		}
	}
	return fmt.Errorf("no conversation where %s=%q", matchField, matchValue)
}

// saveArrayLength saves the length of a JSON array from the last response.
func (tc *testContext) saveArrayLength(arrayPath, key string) error {
	arr := gjson.GetBytes(tc.lastBody, arrayPath)
	if !arr.IsArray() {
		tc.saved[key] = "0"
		return nil
	}
	tc.saved[key] = fmt.Sprintf("%d", len(arr.Array()))
	return nil
}
