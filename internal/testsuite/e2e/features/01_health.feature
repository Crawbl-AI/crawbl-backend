Feature: Health and public endpoints
  As a client application
  I need to check server health and read legal documents
  Before authenticating

  Scenario: Health check returns online status
    When I send a GET request to "/v1/health" without auth
    Then the response status should be 200
    And the response JSON at "data.online" should equal "true"

  Scenario: Public legal documents are available
    When I send a GET request to "/v1/legal" without auth
    Then the response status should be 200
    And the response JSON at "data.terms_of_service" should contain "crawbl.com"
    And the response JSON at "data.privacy_policy" should contain "crawbl.com"
    And the response JSON at "data.terms_of_service_version" should not be empty
    And the response JSON at "data.privacy_policy_version" should not be empty
