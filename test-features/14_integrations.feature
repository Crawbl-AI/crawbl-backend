Feature: Integrations and tools catalog
  As a signed-in user
  I want to browse available integrations and tools
  So I can connect apps and understand agent capabilities

  Background:
    Given user "primary" has signed up

  Scenario: The integrations endpoint returns categories and items
    When user "primary" sends a GET request to "/v1/integrations"
    Then the response status should be 200
    And the response JSON at "data.categories" should not be empty
    And the response JSON array "data.categories" should contain an item where "id" equals "search"
    And the response JSON array "data.categories" should contain an item where "id" equals "utility"
    And the response JSON at "data.items" should not be empty
    And the response JSON array "data.items" should contain an item where "name" equals "Web Search"
    And the response JSON array "data.items" should contain an item where "name" equals "Calculator"
    And the response JSON array "data.items" should contain an item where "type" equals "tool"

  Scenario: Each category has required fields
    When user "primary" sends a GET request to "/v1/integrations"
    Then the response status should be 200
    And the response JSON at "data.categories.0.id" should not be empty
    And the response JSON at "data.categories.0.name" should not be empty
    And the response JSON at "data.categories.0.image_url" should not be empty

  Scenario: Each tool item has required fields
    When user "primary" sends a GET request to "/v1/integrations"
    Then the response status should be 200
    And the response JSON at "data.items.0.name" should not be empty
    And the response JSON at "data.items.0.description" should not be empty
    And the response JSON at "data.items.0.icon_url" should not be empty
    And the response JSON at "data.items.0.type" should not be empty
    And the response JSON at "data.items.0.category_id" should not be empty
