Feature: Multi-user isolation
  As the platform
  I must keep each user's workspace private and independent
  So one account can never see or damage another account's data

  Scenario: Two users receive different default workspaces
    Given an extra test user "frank"
    And an extra test user "grace"
    And user "frank" has signed up
    And user "grace" has signed up
    Then users "frank" and "grace" should have different default workspaces

  Scenario: A user cannot open another user's workspace
    Given an extra test user "frank"
    And an extra test user "grace"
    And user "frank" has signed up
    And user "grace" has signed up
    When user "grace" opens user "frank"'s default workspace
    Then the request should be rejected as not found

  Scenario: A user cannot open another user's agents
    Given an extra test user "frank"
    And an extra test user "grace"
    And user "frank" has signed up
    And user "grace" has signed up
    When user "grace" opens user "frank"'s agents
    Then the request should be rejected as not found

  Scenario: A user cannot open another user's conversations
    Given an extra test user "frank"
    And an extra test user "grace"
    And user "frank" has signed up
    And user "grace" has signed up
    When user "grace" opens user "frank"'s conversations
    Then the request should be rejected as not found

  Scenario: Deleting one account does not affect another active user
    Given an extra test user "frank"
    And an extra test user "grace"
    And user "frank" has signed up
    And user "grace" has signed up
    Then deleting user "frank" should not affect user "grace"
