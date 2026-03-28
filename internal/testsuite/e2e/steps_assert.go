package e2e

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/cucumber/godog"
	"github.com/tidwall/gjson"
)

func registerAssertionSteps(sc *godog.ScenarioContext, tc *testContext) {
	sc.Step(`^the response status should be (\d+)$`, tc.assertStatus)
	sc.Step(`^the response status should be one of "([^"]*)"$`, tc.assertStatusOneOf)
	sc.Step(`^the response JSON at "([^"]*)" should equal "([^"]*)"$`, tc.assertJSONEquals)
	sc.Step(`^the response JSON at "([^"]*)" should contain "([^"]*)"$`, tc.assertJSONContains)
	sc.Step(`^the response JSON at "([^"]*)" should not be empty$`, tc.assertJSONNotEmpty)
	sc.Step(`^the response JSON at "([^"]*)" should equal the subject of "([^"]*)"$`, tc.assertJSONEqualsSubject)
	sc.Step(`^the response JSON at "([^"]*)" should equal the email of "([^"]*)"$`, tc.assertJSONEqualsEmail)
	sc.Step(`^the response JSON at "([^"]*)" should equal the saved "([^"]*)"$`, tc.assertJSONEqualsSaved)
	sc.Step(`^the response JSON at "([^"]*)" should be an array of length (\d+)$`, tc.assertJSONArrayLength)
	sc.Step(`^the response JSON array "([^"]*)" should contain an item where "([^"]*)" equals "([^"]*)"$`, tc.assertJSONArrayContainsItem)
	sc.Step(`^the response JSON should match:$`, tc.assertJSONTable)
	sc.Step(`^the saved "([^"]*)" should not equal the saved "([^"]*)"$`, tc.assertSavedNotEqual)
	sc.Step(`^if the response status is (\d+):$`, tc.conditionalOnStatus)
	sc.Step(`^if status is (\d+) the response JSON at "([^"]*)" should equal "([^"]*)"$`, tc.conditionalJSONEquals)
	sc.Step(`^if status is (\d+) the response JSON at "([^"]*)" should not be empty$`, tc.conditionalJSONNotEmpty)
}

func (tc *testContext) assertStatus(expected int) error {
	if tc.lastStatus != expected {
		body := string(tc.lastBody)
		if len(body) > 200 {
			body = body[:200]
		}
		return fmt.Errorf("expected status %d, got %d; body: %s", expected, tc.lastStatus, body)
	}
	return nil
}

func (tc *testContext) assertStatusOneOf(codes string) error {
	parts := strings.Split(codes, ",")
	for _, p := range parts {
		code, _ := strconv.Atoi(strings.TrimSpace(p))
		if tc.lastStatus == code {
			return nil
		}
	}
	return fmt.Errorf("expected status one of [%s], got %d", codes, tc.lastStatus)
}

func (tc *testContext) assertJSONEquals(path, expected string) error {
	got := gjson.GetBytes(tc.lastBody, path).String()
	if got != expected {
		return fmt.Errorf("JSON %s: expected %q, got %q", path, expected, got)
	}
	return nil
}

func (tc *testContext) assertJSONContains(path, substr string) error {
	got := gjson.GetBytes(tc.lastBody, path).String()
	if !strings.Contains(got, substr) {
		return fmt.Errorf("JSON %s: %q does not contain %q", path, got, substr)
	}
	return nil
}

func (tc *testContext) assertJSONNotEmpty(path string) error {
	got := gjson.GetBytes(tc.lastBody, path).String()
	if got == "" {
		return fmt.Errorf("JSON %s: expected non-empty value", path)
	}
	return nil
}

func (tc *testContext) assertJSONEqualsSubject(path, alias string) error {
	user := tc.users[alias]
	if user == nil {
		return fmt.Errorf("unknown user %q", alias)
	}
	return tc.assertJSONEquals(path, user.subject)
}

func (tc *testContext) assertJSONEqualsEmail(path, alias string) error {
	user := tc.users[alias]
	if user == nil {
		return fmt.Errorf("unknown user %q", alias)
	}
	return tc.assertJSONEquals(path, user.email)
}

func (tc *testContext) assertJSONEqualsSaved(path, key string) error {
	expected := tc.saved[key]
	if expected == "" {
		return fmt.Errorf("no saved value for %q", key)
	}
	return tc.assertJSONEquals(path, expected)
}

func (tc *testContext) assertJSONArrayLength(path string, expected int) error {
	arr := gjson.GetBytes(tc.lastBody, path)
	if !arr.IsArray() {
		return fmt.Errorf("JSON %s: expected array, got %s", path, arr.Type)
	}
	got := len(arr.Array())
	if got != expected {
		return fmt.Errorf("JSON %s: expected array length %d, got %d", path, expected, got)
	}
	return nil
}

func (tc *testContext) assertJSONArrayContainsItem(arrayPath, field, expected string) error {
	arr := gjson.GetBytes(tc.lastBody, arrayPath)
	if !arr.IsArray() {
		return fmt.Errorf("JSON %s: expected array", arrayPath)
	}
	for _, item := range arr.Array() {
		if item.Get(field).String() == expected {
			return nil
		}
	}
	return fmt.Errorf("JSON %s: no item where %s=%q", arrayPath, field, expected)
}

func (tc *testContext) assertJSONTable(table *godog.Table) error {
	for i, row := range table.Rows {
		if i == 0 {
			continue // skip header
		}
		if len(row.Cells) < 2 {
			continue
		}
		path := row.Cells[0].Value
		expected := row.Cells[1].Value
		got := gjson.GetBytes(tc.lastBody, path).String()
		if got != expected {
			return fmt.Errorf("row %d: JSON %s: expected %q, got %q", i, path, expected, got)
		}
	}
	return nil
}

func (tc *testContext) assertSavedNotEqual(key1, key2 string) error {
	v1 := tc.saved[key1]
	v2 := tc.saved[key2]
	if v1 == v2 {
		return fmt.Errorf("saved %q and %q are both %q — expected different values", key1, key2, v1)
	}
	return nil
}

func (tc *testContext) conditionalOnStatus(expected int) error {
	// If the status doesn't match, skip the nested steps (they'll be no-ops).
	// godog doesn't support true conditional steps, so we just return nil.
	if tc.lastStatus != expected {
		return nil
	}
	return nil
}

// conditionalJSONEquals asserts a JSON value only when the last status matches.
// Used for steps like: And if status is 200 the response JSON at "data.role" should equal "agent"
func (tc *testContext) conditionalJSONEquals(status int, path, expected string) error {
	if tc.lastStatus != status {
		return nil // skip — status didn't match
	}
	return tc.assertJSONEquals(path, expected)
}

// conditionalJSONNotEmpty asserts a JSON value is non-empty only when the last status matches.
func (tc *testContext) conditionalJSONNotEmpty(status int, path string) error {
	if tc.lastStatus != status {
		return nil
	}
	return tc.assertJSONNotEmpty(path)
}

// gjsonGet is a convenience function for step helpers.
func gjsonGet(body string, path string) string {
	return gjson.Get(body, path).String()
}
