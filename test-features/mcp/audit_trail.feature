Feature: Orchestrator tool audit trail
  As a platform operator
  I want every orchestrator tool call to be recorded in the audit trail
  So I can track what actions agents performed on behalf of users

  Background:
    Given user "primary" has signed up
    And user "primary" opens their default workspace
    And user "primary" opens the swarm conversation
    And user "primary" waits until their assistant is ready

  Scenario: The assistant looking up the user profile leaves an audit record
    When user "primary" sends the message "What is my profile name and email address?" in the current conversation
    Then the assistant reply should succeed
    And the assistant reply should contain text
    And the audit trail should include a "get_user_profile" tool call for subject "primary"

  Scenario: The assistant listing conversations leaves an audit record
    When user "primary" sends the message "How many conversations do I have in this workspace? Please check." in the current conversation
    Then the assistant reply should succeed
    And the assistant reply should contain text
    And the audit trail should include a "list_conversations" tool call for subject "primary"
