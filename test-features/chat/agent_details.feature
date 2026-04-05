Feature: Agent profile and configuration
  As a signed-in user
  I want to view each agent's details, history, settings, and tools
  So I understand what my assistants can do and how they are configured

  Background:
    Given user "primary" has signed up
    And user "primary" opens their default workspace
    And user "primary" opens the agents in their default workspace

  Scenario: A user can view an agent's detailed profile
    When user "primary" opens the details for agent "wally"
    Then the response status should be 200
    And the response JSON at "data.name" should not be empty

  Scenario: A user can view an agent's history
    When user "primary" opens the history for agent "wally"
    Then the response status should be 200

  Scenario: A user can view an agent's settings
    When user "primary" opens the settings for agent "wally"
    Then the response status should be 200

  Scenario: A user can view an agent's available tools
    When user "primary" opens the tools for agent "wally"
    Then the response status should be 200
