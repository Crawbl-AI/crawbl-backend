Feature: Real error handling
  As the platform
  I need realistic failure cases to behave cleanly
  So clients can recover without guesswork

  Background:
    Given user "primary" has signed up

  Scenario: A guest cannot read a protected profile
    When a guest requests their profile
    Then the request should be unauthorized

  Scenario: A missing workspace returns not found
    When user "primary" opens a workspace that does not exist
    Then the request should be rejected as not found

  Scenario: A missing conversation returns not found
    When user "primary" opens a conversation that does not exist in their default workspace
    Then the request should be rejected as not found

  Scenario: The client can see runtime information for a workspace
    When user "primary" opens their default workspace
    Then user "primary" should see runtime details for their default workspace
