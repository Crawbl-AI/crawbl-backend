Feature: Agent memories from the app
  As a signed-in user
  I want to save, view, and remove memories for an agent from the app
  So I can manually teach my assistant things it should remember

  Background:
    Given user "primary" has signed up
    And user "primary" opens their default workspace
    And user "primary" opens the agents in their default workspace

  Scenario: A user can save a memory against an agent from the app
    When user "primary" saves a memory with key "fav_color" and content "blue" for agent "wally"
    Then the response status should be 204

  Scenario: A user can list memories for an agent
    When user "primary" opens the memories for agent "wally"
    Then the response status should be 200

  Scenario: A user can remove a memory from an agent
    When user "primary" deletes the memory with key "fav_color" for agent "wally"
    Then the response status should be 204
