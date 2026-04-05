Feature: Direct agent conversations
  As a mobile app user
  I want focused one-to-one threads with each assistant
  So work stays organized per agent

  Background:
    Given user "primary" has signed up
    And user "primary" opens their default workspace
    And user "primary" waits until their assistant is ready

  Scenario: Wally's work stays in Wally's thread
    When user "primary" opens the "wally" direct conversation
    Then the current conversation should belong to the "wally" agent
    When user "primary" sends the message "Say you are ready in four words." in the current conversation
    Then the assistant reply should succeed
    And the assistant reply should come from the "wally" agent
