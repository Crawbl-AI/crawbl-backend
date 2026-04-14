Feature: Agent memory persistence
  As a signed-in user
  I want my assistant to remember facts I ask it to save
  So it can recall them in later conversations without me repeating myself

  Background:
    Given user "primary" has signed up
    And user "primary" opens their default workspace
    And user "primary" opens the swarm conversation
    And user "primary" waits until their assistant is ready

  @llm-flaky
  Scenario: The assistant can remember a fact between messages
    When user "primary" mentions the "wally" agent in the swarm conversation saying "Please save the following note to your long-term memory with key trip_budget: We set the Paris trip budget at three thousand euros."
    Then the assistant reply should succeed
    And the assistant reply should contain text
    And the assistant should have remembered at least 1 note for subject "primary"

  @llm-flaky
  Scenario: The assistant can recall a previously saved fact
    Given user "primary" has saved a memory with key "trip_budget" and content "We set the Paris trip budget at three thousand euros" for agent "wally"
    When user "primary" mentions the "wally" agent in the swarm conversation saying "Please recall from your memory what the Paris trip budget is."
    Then the assistant reply should succeed
    And the assistant reply should contain text
    And the assistant reply should mention one of "three thousand|3000|3,000"
    And the assistant reply should mention "paris"

  Scenario: The assistant can forget a saved fact
    Given user "primary" has saved a memory with key "trip_budget" and content "We set the Paris trip budget at three thousand euros" for agent "wally"
    When user "primary" mentions the "wally" agent in the swarm conversation saying "Please forget the memory with key trip_budget."
    Then the assistant reply should succeed
    And the assistant reply should contain text
    And the memory with key "trip_budget" should no longer exist for agent "wally"

  Scenario: Previously saved memories persist across conversations
    Given user "primary" has saved a memory with key "fav_city" and content "Tokyo" for agent "wally"
    When user "primary" opens the memories for agent "wally"
    Then the response status should be 200
    And the assistant should have remembered at least 1 note for subject "primary"
