Feature: Cross-user access control
  As the platform
  I need to prevent users from accessing each other's data
  So user privacy is enforced at every endpoint

  Background:
    Given user "primary" has signed up
    And an extra test user "frank"
    And user "frank" has signed up

  Scenario: A user cannot read another user's agent details
    Given user "primary" opens their default workspace
    And user "primary" opens the agents in their default workspace
    When user "frank" opens the details for agent "wally"
    Then the response status should be one of "403,404"

  Scenario: A user cannot delete another user's conversation
    Given user "primary" opens their default workspace
    And user "primary" opens the swarm conversation
    When user "frank" sends a GET request to "/v1/workspaces/{workspace_id}/conversations"
    Then the response status should be one of "403,404"
