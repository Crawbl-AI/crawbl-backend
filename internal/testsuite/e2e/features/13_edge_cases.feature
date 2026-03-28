Feature: Edge cases and error handling
  As the platform
  I need to handle invalid requests gracefully
  And return proper error codes

  Background:
    Given the primary test user has signed up

  Scenario: Access non-existent workspace returns 404
    When user "primary" sends a GET request to "/v1/workspaces/00000000-0000-0000-0000-000000000000"
    Then the response status should be 404

  Scenario: Access non-existent conversation returns 404
    Given user "primary" has a workspace saved as "workspace_id"
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}/conversations/00000000-0000-0000-0000-000000000000"
    Then the response status should be 404

  Scenario: Send message to non-existent conversation returns 404
    Given user "primary" has a workspace saved as "workspace_id"
    When user "primary" sends a POST request to "/v1/workspaces/{workspace_id}/conversations/00000000-0000-0000-0000-000000000000/messages" with JSON:
      """
      {"local_id": "e2e-404-msg", "content": {"type": "text", "text": "should fail"}, "attachments": []}
      """
    Then the response status should be 404

  Scenario: Send message with unsupported content type returns 400
    Given user "primary" has a workspace saved as "workspace_id"
    And user "primary" has a conversation saved as "conversation_id"
    When user "primary" sends a POST request to "/v1/workspaces/{workspace_id}/conversations/{conversation_id}/messages" with JSON:
      """
      {"local_id": "e2e-bad-type", "content": {"type": "action_card", "title": "nope"}, "attachments": []}
      """
    Then the response status should be 400

  Scenario: Empty PATCH to profile returns 204 (no-op)
    When user "primary" sends a PATCH request to "/v1/users" with JSON:
      """
      {}
      """
    Then the response status should be 204

  Scenario: Unauthenticated request to protected endpoint returns 401
    When I send a GET request to "/v1/users/profile" without auth
    Then the response status should be 401

  Scenario: Workspace runtime status reflects actual state
    Given user "primary" has a workspace saved as "workspace_id"
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}"
    Then the response status should be 200
    And the response JSON at "data.runtime" should not be empty
    And the response JSON at "data.runtime.status" should not be empty
    And the response JSON at "data.runtime.phase" should not be empty
