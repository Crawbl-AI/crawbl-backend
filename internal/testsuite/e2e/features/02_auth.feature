Feature: Authentication
  As a mobile app user
  I need to sign up and sign in
  So I can access the platform

  Background:
    Given a new test user "alice"

  Scenario: New user sign-up creates account and default workspace
    When user "alice" sends a POST request to "/v1/auth/sign-up"
    Then the response status should be 204
    And the database should have a user with subject "alice"
    And the database should have 1 workspace for subject "alice"
    # Agents and conversations are bootstrapped on first workspace/conversation access,
    # not during sign-up. Verify them via the API instead.
    When user "alice" sends a GET request to "/v1/workspaces"
    Then the response status should be 200
    And the response JSON at "data" should be an array of length 1

  Scenario: Existing user can sign in
    Given user "alice" has signed up
    When user "alice" sends a POST request to "/v1/auth/sign-in"
    Then the response status should be 204

  Scenario: Sign-up is idempotent
    Given user "alice" has signed up
    When user "alice" sends a POST request to "/v1/auth/sign-up"
    Then the response status should be 204
    And the database should have 1 workspace for subject "alice"
