Feature: Assistant configuration reflects the latest settings
  As a signed-in user
  I want changes to my assistant's configuration to be reflected immediately
  So that the assistant always runs with the settings I have chosen

  Background:
    Given user "primary" has signed up
    And user "primary" opens their default workspace

  @db
  Scenario: Changing an assistant's model updates the assistant's current configuration
    When user "primary" sets the model for agent "wally" to "gpt-5-mini"
    Then the assistant's current configuration for agent "wally" should report model "gpt-5-mini"

  @db
  Scenario: Adding a new tool to an assistant updates its tool list
    When user "primary" adds tool "web_search_tool" to agent "wally"
    Then the assistant's current tool list for agent "wally" should include "web_search_tool"
