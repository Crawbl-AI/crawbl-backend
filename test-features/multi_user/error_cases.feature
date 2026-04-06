Feature: Cross-user access control edge cases
  As the platform
  I need to prevent users from accessing each other's data
  So user privacy is enforced at every endpoint

  Background:
    Given user "primary" has signed up
    And an extra test user "frank"
    And user "frank" has signed up

  Scenario: A user cannot access another user's workspace by identifier
    Given user "primary" opens their default workspace
    When user "frank" opens user "primary"'s default workspace
    Then the response status should be one of "403,404"

  Scenario: A user cannot list another user's conversations
    Given user "primary" opens their default workspace
    When user "frank" opens user "primary"'s conversations
    Then the response status should be one of "403,404"
