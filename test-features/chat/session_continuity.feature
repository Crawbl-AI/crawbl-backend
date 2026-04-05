Feature: Conversation context across turns
  As a signed-in user
  I want my assistant to remember what we discussed earlier in the conversation
  So I do not have to repeat myself every message

  Background:
    Given user "primary" has signed up
    And user "primary" opens their default workspace
    And user "primary" opens the swarm conversation
    And user "primary" waits until their assistant is ready

  Scenario: The assistant remembers the current conversation context across turns
    When user "primary" sends the message "My name is TestUser and I am planning a trip to Tokyo." in the current conversation
    Then the assistant reply should succeed
    And the assistant reply should contain text
    When user "primary" sends the message "What city am I planning to visit?" in the current conversation
    Then the assistant reply should succeed
    And the assistant reply should contain text
    And the assistant should remember the current conversation context
    And the assistant session should expire automatically
