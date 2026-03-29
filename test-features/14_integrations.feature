Feature: Integrations and tools catalog
  As a signed-in user
  I want to browse available integrations and tools
  So I can connect apps and understand agent capabilities

  Background:
    Given user "primary" has signed up

  Scenario: The catalog includes tool categories
    When user "primary" opens the integrations catalog
    Then user "primary" should see tool categories

  Scenario: The catalog includes agent tools
    When user "primary" opens the integrations catalog
    Then user "primary" should see tools in the catalog

  Scenario: The catalog includes third-party integration apps
    When user "primary" opens the integrations catalog
    Then user "primary" should see integration apps in the catalog
