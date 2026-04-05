Feature: Everyday messaging
  As a signed-in user
  I want my first real message to wait for the assistant to be ready
  So chat works like a normal product flow instead of a race

  Background:
    Given user "primary" has signed up
    And user "primary" opens their default workspace
    And user "primary" opens the swarm conversation
    And user "primary" waits until their assistant is ready

  Scenario: A user can send a message once the assistant is ready
    When user "primary" sends the message "Hello. Please introduce yourself in one sentence." in the current conversation
    Then the assistant reply should succeed
    And the assistant reply should contain text
    And the assistant reply should come from an agent

  Scenario: The conversation exposes pagination metadata
    When user "primary" opens the messages in the current conversation
    Then the current conversation should expose pagination metadata

  Scenario: Empty messages are rejected
    When user "primary" sends an empty message in the current conversation
    Then the request should be rejected as invalid
