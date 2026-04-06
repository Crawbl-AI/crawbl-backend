Feature: Chat error handling
  As a signed-in user
  I want clear errors when I misuse chat endpoints
  So my app can show helpful messages instead of crashing

  Background:
    Given user "primary" has signed up
    And user "primary" opens their default workspace

  Scenario: Sending a message to a non-existent conversation fails
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}/conversations/00000000-0000-0000-0000-000000000000/messages"
    Then the response status should be 404

  Scenario: Opening messages without a valid conversation returns not found
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}/conversations/not-a-uuid/messages"
    Then the response status should be one of "400,404"

  Scenario: Creating a conversation with an invalid type is rejected
    When user "primary" sends a POST request to "/v1/workspaces/{workspace_id}/conversations" with JSON:
      """
      {"type": "invalid_type", "title": "Bad type"}
      """
    Then the response status should be 400
