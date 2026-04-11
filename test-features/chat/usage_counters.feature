Feature: Agent token usage tracking
  As a platform operator
  I want the assistant's activity to be reflected in usage counters
  So that consumption is visible and billing-relevant data is accurate

  Background:
    Given user "primary" has signed up
    And user "primary" opens their default workspace

  Scenario: A fresh user starts with zero recorded token usage
    Given user "primary" has no recorded usage for agent "manager"
    Then the assistant's recorded usage should be zero for agent "manager"

  @llm-flaky
  Scenario: Sending a message increments the agent's token counter
    Given user "primary" waits until their assistant is ready
    And the assistant's usage counter for agent "manager" should be captured as the baseline
    When user "primary" opens the swarm conversation
    And user "primary" sends the message "Reply with exactly the word 'token'." in the current conversation
    Then the assistant reply should succeed
    And the assistant reply should contain text
    And the assistant should have consumed at least 1 token more than before within 30 seconds

  @llm-flaky
  Scenario: The workspace usage summary reflects recent activity
    Given user "primary" waits until their assistant is ready
    And user "primary" opens the swarm conversation
    And user "primary" sends the message "Reply with exactly the word 'usage'." in the current conversation
    And the assistant reply should succeed
    Then the workspace usage summary should show recent activity
