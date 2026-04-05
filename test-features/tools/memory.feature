Feature: Agent memory persistence
  As a signed-in user
  I want my assistant to remember facts I ask it to save
  So it can recall them in later conversations without me repeating myself

  Background:
    Given user "primary" has signed up
    And user "primary" opens their default workspace
    And user "primary" opens the swarm conversation
    And user "primary" waits until their assistant is ready

  Scenario: The assistant can remember a fact between messages
    When user "primary" mentions the "wally" agent in the swarm conversation saying "Please save the following note to your long-term memory with key trip_budget: We set the Paris trip budget at three thousand euros."
    Then the assistant reply should succeed
    And the assistant reply should contain text
    And the assistant should have remembered at least 1 note for subject "primary"

  Scenario: The assistant can recall a previously saved fact
    When user "primary" mentions the "wally" agent in the swarm conversation saying "Please recall from your memory what the Paris trip budget is."
    Then the assistant reply should succeed
    And the assistant reply should contain text

  Scenario: The assistant can forget a saved fact
    When user "primary" mentions the "wally" agent in the swarm conversation saying "Please forget the memory with key trip_budget."
    Then the assistant reply should succeed
    And the assistant reply should contain text
