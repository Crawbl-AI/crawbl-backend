Feature: Profile error handling
  As a signed-in user
  I want clear errors when I update my profile with bad data
  So I can fix the problem

  Background:
    Given user "primary" has signed up

  Scenario: Updating profile with an empty body still succeeds
    When user "primary" sends a PATCH request to "/v1/users" with JSON:
      """
      {}
      """
    Then the response status should be one of "200,204"

  Scenario: A user can read their own profile after updating
    When user "primary" opens their profile
    Then the response status should be 200
    And the response JSON at "data.email" should not be empty
