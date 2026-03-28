Feature: Account deletion
  As a user
  I need to delete my account
  And have my data marked as deleted

  Background:
    Given a new test user "zach"
    And user "zach" has signed up

  Scenario: Delete account with reason
    When user "zach" sends a DELETE request to "/v1/auth/delete" with JSON:
      """
      {"reason": "e2e-cleanup", "description": "automated test cleanup"}
      """
    Then the response status should be 204
    And the database user "zach" should have deleted_at set
    And the database user "zach" should have is_deleted "true"

  Scenario: Deleted user profile shows deleted flag
    Given user "zach" has deleted their account
    When user "zach" sends a GET request to "/v1/users/profile"
    Then the response status should be one of "200,403"
    And if the response status is 200:
      Then the response JSON at "data.is_deleted" should equal "true"
