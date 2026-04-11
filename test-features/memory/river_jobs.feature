Feature: Background processing cycles
  As a platform operator
  I want background processing cycles to run reliably
  So that usage is recorded, stale data is cleaned up, and memory stays healthy

  Scenario: Usage is recorded after an assistant reply
    Given user "primary" has signed up
    And user "primary" opens their default workspace
    And user "primary" opens the swarm conversation
    And user "primary" waits until their assistant is ready
    When user "primary" sends the message "Hello." in the current conversation
    Then the assistant reply should succeed
    And the assistant's usage should be recorded within 60 seconds

  Scenario: Stale pending messages are eventually cleaned up
    Given user "primary" has signed up
    And user "primary" opens their default workspace
    And user "primary" opens the swarm conversation
    And user "primary" waits until their assistant is ready
    When user "primary" sends the message "Hello." in the current conversation
    Then the assistant reply should succeed
    And the message cleanup cycle should complete within 120 seconds

  Scenario: The memory maintenance cycle runs on schedule
    Given user "primary" has signed up
    And user "primary" opens their default workspace
    And user "primary" opens the swarm conversation
    And user "primary" waits until their assistant is ready
    When user "primary" sends the message "Hello." in the current conversation
    Then the assistant reply should succeed
    And a memory maintenance cycle should complete within 180 seconds
