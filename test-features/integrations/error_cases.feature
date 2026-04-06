Feature: Integration error handling
  As a signed-in user
  I want clear errors when integration operations fail
  So I know what to fix

  Background:
    Given user "primary" has signed up

  Scenario: Integration callback without valid state fails
    When user "primary" sends a POST request to "/v1/integrations/callback" with JSON:
      """
      {"code": "fake_code", "state": "invalid_state"}
      """
    Then the response status should be one of "400,404"
