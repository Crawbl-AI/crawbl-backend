Feature: Authentication
  As a mobile app user
  I need to sign up and sign in
  So I can access the platform

  # All auth scenarios use the shared primary user (1 UserSwarm for all).

  Scenario: New user sign-up creates account and default workspace
    Given the primary test user
    When user "primary" sends a POST request to "/v1/auth/sign-up"
    Then the response status should be 204
    And the database should have a user with subject "primary"
    And the database should have 1 workspace for subject "primary"
    When user "primary" sends a GET request to "/v1/workspaces"
    Then the response status should be 200
    And the response JSON at "data" should be an array of length 1

  Scenario: Existing user can sign in
    Given the primary test user has signed up
    When user "primary" sends a POST request to "/v1/auth/sign-in"
    Then the response status should be 204

  Scenario: Sign-up is idempotent
    Given the primary test user has signed up
    When user "primary" sends a POST request to "/v1/auth/sign-up"
    Then the response status should be 204
    And the database should have 1 workspace for subject "primary"
