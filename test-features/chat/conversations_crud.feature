Feature: Conversation management
  As a signed-in user
  I want to create, search, mark as read, and delete conversations
  So I can organise my workspace and find past discussions

  Background:
    Given user "primary" has signed up
    And user "primary" opens their default workspace

  Scenario: A user can create a new conversation
    When user "primary" creates a conversation named "Scratch pad" in their default workspace
    Then the response status should be one of "200,201"
    And the response JSON at "data.title" should not be empty

  Scenario: A user can mark a conversation as read
    When user "primary" opens the swarm conversation
    And user "primary" marks the current conversation as read
    Then the response status should be 200

  Scenario: Message search indicates it is not yet available
    Given user "primary" opens the swarm conversation
    When user "primary" searches messages for "pineapple" in the current conversation
    Then the response status should be 501

  Scenario: A user can delete a scratch conversation
    When user "primary" creates a conversation named "Temporary" in their default workspace
    Then the response status should be 200
    When user "primary" deletes the current conversation
    Then the response status should be 200
    When user "primary" opens the current conversation again
    Then the request should be rejected as not found
