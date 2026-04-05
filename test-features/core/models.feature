Feature: Language model catalog
  As a signed-in user
  I want to see which language models are available on the platform
  So I understand what powers my assistants

  Scenario: The app can list available language models
    When I send a GET request to "/v1/models" without auth
    Then the response status should be 200
    And the response JSON at "data" should not be empty
