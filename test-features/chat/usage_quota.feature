Feature: Monthly message limits
  As a signed-in user
  I want my monthly message limit to be enforced
  So that platform resources are allocated fairly

  Background:
    Given user "primary" has signed up
    And user "primary" opens their default workspace
    And user "primary" opens the swarm conversation
    And user "primary" waits until their assistant is ready

  @db
  Scenario: A user without any monthly limit can send messages freely
    Given user "primary" has no monthly limit
    When user "primary" sends the message "Hello, how are you?" in the current conversation
    Then the assistant reply should succeed

  @db
  Scenario: A user within their monthly limit can send messages
    Given user "primary" has a monthly limit of 100000 tokens
    And user "primary" has already used 50 tokens this month
    When user "primary" sends the message "Hello, how are you?" in the current conversation
    Then the assistant reply should succeed

  @db
  Scenario: A user who has reached their monthly limit is blocked
    Given user "primary" has a monthly limit of 1000 tokens
    And user "primary" has already used 1500 tokens this month
    When user "primary" sends the message "Hello, how are you?" in the current conversation
    Then the request should be rejected with a quota-exceeded error
