Feature: Legal agreement acceptance
  As a mobile app user
  I must accept terms of service and privacy policy
  Before I can fully use the platform

  Background:
    Given the primary test user has signed up

  Scenario: Legal status shows acceptance state
    When user "primary" sends a GET request to "/v1/users/legal"
    Then the response status should be 200
    And the response JSON at "data.terms_of_service_version" should not be empty
    And the response JSON at "data.privacy_policy_version" should not be empty

  Scenario: Accept terms of service
    When user "primary" sends a POST request to "/v1/users/legal/accept" with JSON:
      """
      {"terms_of_service_version": "v1"}
      """
    Then the response status should be 204
    When user "primary" sends a GET request to "/v1/users/legal"
    Then the response JSON at "data.has_agreed_with_terms" should equal "true"

  Scenario: Accept privacy policy
    When user "primary" sends a POST request to "/v1/users/legal/accept" with JSON:
      """
      {"privacy_policy_version": "v1"}
      """
    Then the response status should be 204
    When user "primary" sends a GET request to "/v1/users/legal"
    Then the response JSON at "data.has_agreed_with_privacy_policy" should equal "true"
