package e2e

import (
	"fmt"

	"github.com/cucumber/godog"
	"github.com/tidwall/gjson"
)

func registerStateSteps(sc *godog.ScenarioContext, tc *testContext) {
	sc.Step(`^I save "([^"]*)" as "([^"]*)"$`, tc.saveJSONValue)
	sc.Step(`^I save the first item in "([^"]*)" where "([^"]*)" equals "([^"]*)" field "([^"]*)" as "([^"]*)"$`, tc.saveFirstMatchingItem)
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
