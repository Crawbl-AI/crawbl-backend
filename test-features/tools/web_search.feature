Feature: Web search capability
  As a signed-in user
  I want my assistant to search the web for current information
  So I get up-to-date answers instead of stale training data

  Background:
    Given user "primary" has signed up
    And user "primary" opens their default workspace
    And user "primary" opens the swarm conversation
    And user "primary" waits until their assistant is ready

  Scenario: The assistant can search the web for current information
    When user "primary" mentions the "wally" agent in the swarm conversation saying "Please use your web search tool to find information about the Go programming language ADK framework by Google."
    Then the assistant reply should succeed
    And the assistant reply should contain text
    And the assistant reply should come from the "wally" agent
