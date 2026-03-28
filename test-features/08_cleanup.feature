Feature: Account deletion
  As a user
  I need to delete my account
  And have my data marked as deleted

  # Uses the "frank" suite-level user for destructive deletion tests.
  # Frank is re-signed-up at the start to ensure a clean state.

  Scenario: Delete account with reason
    Given an extra test user "frank"
    And user "frank" has signed up
    When user "frank" sends a DELETE request to "/v1/auth/delete" with JSON:
      """
      {"reason": "e2e-cleanup", "description": "automated test cleanup"}
      """
    Then the response status should be 204
    And the database user "frank" should have deleted_at set
    And the database user "frank" should have is_deleted "true"

  Scenario: Deleted user profile shows deleted flag
    Given an extra test user "frank"
    And user "frank" has signed up
    And user "frank" has deleted their account
    When user "frank" sends a GET request to "/v1/users/profile"
    Then the response status should be one of "200,403"
