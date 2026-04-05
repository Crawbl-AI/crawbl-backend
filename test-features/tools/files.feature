Feature: Workspace file storage
  As a signed-in user
  I want my assistant to save and read files in my workspace
  So I can work with documents across conversations

  Background:
    Given user "primary" has signed up
    And user "primary" opens their default workspace
    And user "primary" opens the swarm conversation
    And user "primary" waits until their assistant is ready

  Scenario: The assistant can save a note as a workspace file
    When user "primary" mentions the "wally" agent in the swarm conversation saying "Please use your file write tool to save a file called e2e_trip.md with the content: Paris trip planning - budget is 3000 euros, departure in June."
    Then the assistant reply should succeed
    And the assistant reply should contain text
    And a file named "e2e_trip.md" should exist in the workspace file store

  Scenario: The assistant can read back a saved file
    When user "primary" mentions the "wally" agent in the swarm conversation saying "Please use your file read tool to read the file e2e_trip.md and tell me what it says."
    Then the assistant reply should succeed
    And the assistant reply should contain text
