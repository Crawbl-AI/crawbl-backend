Feature: File upload contract
  As a platform developer
  I want the upload endpoint to clearly indicate it is not yet available
  So mobile clients handle the limitation gracefully

  Background:
    Given user "primary" has signed up

  Scenario: File uploads are not yet available
    When user "primary" sends a POST request to "/v1/uploads"
    Then the response status should be 501
