Feature: Sign out
  As a signed-in user
  I want to sign out cleanly
  So my session is closed properly

  Background:
    Given user "primary" has signed up

  Scenario: A user can sign out
    When user "primary" sends a POST request to "/v1/auth/logout"
    Then the response status should be one of "200,204"
