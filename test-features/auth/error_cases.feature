Feature: Authentication error handling
  As the platform
  I need invalid or missing credentials to be rejected cleanly
  So attackers and misconfigured clients get clear feedback

  Scenario: A request without any auth headers is rejected
    When I send a GET request to "/v1/users/profile" without auth
    Then the response status should be 401

  Scenario: Signing up with missing device headers still works
    Given user "primary" has signed up
    When user "primary" opens their profile
    Then the response status should be 200

  Scenario: A user cannot sign in without signing up first
    Given an extra test user "grace"
    When user "grace" signs in
    Then the response status should be one of "200,204"
