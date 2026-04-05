package e2e

import (
	"fmt"

	"github.com/cucumber/godog"
	"github.com/tidwall/gjson"
)

func registerIntegrationSteps(sc *godog.ScenarioContext, tc *testContext) {
	sc.Step(`^user "([^"]*)" opens the integrations catalog$`, tc.userOpensIntegrationsCatalog)
	sc.Step(`^user "([^"]*)" should see tool categories$`, tc.userShouldSeeToolCategories)
	sc.Step(`^user "([^"]*)" should see tools in the catalog$`, tc.userShouldSeeToolsInCatalog)
	sc.Step(`^user "([^"]*)" should see integration apps in the catalog$`, tc.userShouldSeeIntegrationAppsInCatalog)
}

func (tc *testContext) userOpensIntegrationsCatalog(alias string) error {
	if _, err := tc.doRequest("GET", "/v1/integrations", alias, nil); err != nil {
		return err
	}
	return tc.assertStatus(200)
}

func (tc *testContext) userShouldSeeToolCategories(alias string) error {
	if err := tc.userOpensIntegrationsCatalog(alias); err != nil {
		return err
	}
	categories := gjson.GetBytes(tc.lastBody, "data.categories")
	if !categories.IsArray() || len(categories.Array()) == 0 {
		return fmt.Errorf("expected non-empty categories array")
	}
	first := categories.Array()[0]
	for _, field := range []string{"id", "name"} {
		if first.Get(field).String() == "" {
			return fmt.Errorf("category missing required field %q", field)
		}
	}
	return nil
}

func (tc *testContext) userShouldSeeToolsInCatalog(alias string) error {
	if err := tc.userOpensIntegrationsCatalog(alias); err != nil {
		return err
	}
	return tc.findCatalogItem("tool", []string{"name", "description", "icon_url", "category_id"})
}

func (tc *testContext) userShouldSeeIntegrationAppsInCatalog(alias string) error {
	if err := tc.userOpensIntegrationsCatalog(alias); err != nil {
		return err
	}
	return tc.findCatalogItem("app", []string{"name", "description", "icon_url", "provider", "category_id"})
}

// findCatalogItem locates the first item of the given type in the
// response body and verifies it has all required fields.
func (tc *testContext) findCatalogItem(itemType string, requiredFields []string) error {
	items := gjson.GetBytes(tc.lastBody, "data.items")
	if !items.IsArray() || len(items.Array()) == 0 {
		return fmt.Errorf("expected non-empty items array")
	}
	for _, item := range items.Array() {
		if item.Get("type").String() == itemType {
			for _, field := range requiredFields {
				if item.Get(field).String() == "" {
					return fmt.Errorf("%s item missing required field %q", itemType, field)
				}
			}
			return nil
		}
	}
	return fmt.Errorf("no items with type=%s found", itemType)
}
