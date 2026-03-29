Feature: Direct agent conversations
  As a mobile app user
  I want focused one-to-one threads with each assistant
  So research work and writing work do not get mixed together

  Background:
    Given user "primary" has signed up
    And user "primary" opens their default workspace
    And user "primary" waits until their assistant is ready

  Scenario: Research work stays in the researcher thread
    When user "primary" opens the "researcher" direct conversation
    Then the current conversation should belong to the "researcher" agent
    When user "primary" sends the message "Say you are ready in four words." in the current conversation
    Then the assistant reply should succeed
    And the assistant reply should come from the "researcher" agent

  Scenario: Writing work stays in the writer thread
    When user "primary" opens the "writer" direct conversation
    Then the current conversation should belong to the "writer" agent
    When user "primary" sends the message "Say you are ready in four words." in the current conversation
    Then the assistant reply should succeed
    And the assistant reply should come from the "writer" agent
