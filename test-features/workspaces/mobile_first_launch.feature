Feature: Mobile first-launch journey
  As a brand new mobile app user
  I want the first launch to move from public checks to a working first chat
  So the product feels ready from the first session

  @llm-flaky
  Scenario: A new user completes first launch and starts chatting
    When the guest checks the service health
    Then the service should report online
    When the guest reads the public legal documents
    Then the public legal documents should be available
    When user "primary" signs up
    And user "primary" accepts the current legal documents
    And user "primary" opens their default workspace
    And user "primary" opens the agents in their default workspace
    And user "primary" opens the conversations in their default workspace
    And user "primary" registers a push token
    And user "primary" opens the swarm conversation
    And user "primary" waits until their assistant is ready
    And user "primary" sends the message "Say hello in one short sentence." in the current conversation
    Then the assistant reply should succeed
    And the assistant reply should contain text
    And the assistant reply should mention "hello"
    And the assistant reply should come from an agent

  Scenario: A returning user can get back to their workspace quickly
    Given user "primary" has signed up
    And user "primary" accepts the current legal documents
    When user "primary" signs in
    And user "primary" opens their default workspace
    And user "primary" opens the agents in their default workspace
    And user "primary" opens the conversations in their default workspace
    Then user "primary" should have a single default workspace

  Scenario: A cold workspace becomes ready before the first chat
    Given user "primary" has signed up
    When user "primary" opens their default workspace
    And user "primary" waits until their assistant is ready
    Then user "primary" should see their workspace runtime as ready
