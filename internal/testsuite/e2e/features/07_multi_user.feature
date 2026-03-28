Feature: Multi-user isolation
  As the platform
  I must ensure users cannot see each other's data
  And each user has isolated workspaces, conversations, and messages

  Scenario: Two users have separate workspaces
    Given a new test user "frank"
    And a new test user "grace"
    And user "frank" has signed up
    And user "grace" has signed up
    When user "frank" sends a GET request to "/v1/workspaces"
    And I save "data.0.id" as "frank_workspace"
    When user "grace" sends a GET request to "/v1/workspaces"
    And I save "data.0.id" as "grace_workspace"
    Then the saved "frank_workspace" should not equal the saved "grace_workspace"

  Scenario: User cannot access another user's workspace
    Given a new test user "frank"
    And a new test user "grace"
    And user "frank" has signed up
    And user "grace" has signed up
    When user "frank" sends a GET request to "/v1/workspaces"
    And I save "data.0.id" as "frank_workspace"
    When user "grace" sends a GET request to "/v1/workspaces/{frank_workspace}"
    Then the response status should be 404

  Scenario: User cannot list another user's agents
    Given a new test user "frank"
    And a new test user "grace"
    And user "frank" has signed up
    And user "grace" has signed up
    When user "frank" sends a GET request to "/v1/workspaces"
    And I save "data.0.id" as "frank_workspace"
    When user "grace" sends a GET request to "/v1/workspaces/{frank_workspace}/agents"
    Then the response status should be 404

  Scenario: User cannot list another user's conversations
    Given a new test user "frank"
    And a new test user "grace"
    And user "frank" has signed up
    And user "grace" has signed up
    When user "frank" sends a GET request to "/v1/workspaces"
    And I save "data.0.id" as "frank_workspace"
    When user "grace" sends a GET request to "/v1/workspaces/{frank_workspace}/conversations"
    Then the response status should be 404

  Scenario: Deleting one user does not affect another
    Given a new test user "frank"
    And a new test user "grace"
    And user "frank" has signed up
    And user "grace" has signed up
    When user "frank" sends a DELETE request to "/v1/auth/delete" with JSON:
      """
      {"reason": "e2e-test", "description": "multi-user isolation test"}
      """
    Then the response status should be 204
    When user "grace" sends a GET request to "/v1/users/profile"
    Then the response status should be 200
    And the response JSON at "data.is_deleted" should equal "false"

  Scenario: Each user gets their own set of agents
    Given a new test user "frank"
    And a new test user "grace"
    And user "frank" has signed up
    And user "grace" has signed up
    When user "frank" sends a GET request to "/v1/workspaces"
    And I save "data.0.id" as "frank_workspace"
    When user "grace" sends a GET request to "/v1/workspaces"
    And I save "data.0.id" as "grace_workspace"
    When user "frank" sends a GET request to "/v1/workspaces/{frank_workspace}/agents"
    Then the response status should be 200
    And the response JSON at "data" should be an array of length 2
    When user "grace" sends a GET request to "/v1/workspaces/{grace_workspace}/agents"
    Then the response status should be 200
    And the response JSON at "data" should be an array of length 2
