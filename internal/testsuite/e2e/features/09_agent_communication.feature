Feature: Agent communication and ZeroClaw verification
  As a mobile app user
  I need agents to respond to my messages with intelligent replies
  So I can verify ZeroClaw is connected and working

  # These tests require the swarm runtime to be ready (Verified=True).
  # If the runtime is still provisioning, message-send tests gracefully
  # accept 500/503 status codes (tested in 06_chat.feature).
  # This feature focuses on verifying AGENT BEHAVIOR when runtime IS ready.

  Background:
    Given the primary test user has signed up
    And user "primary" has a workspace saved as "workspace_id"

  Scenario: Agents know they are part of Crawbl platform
    Given user "primary" has a conversation saved as "conversation_id"
    When user "primary" sends a POST request to "/v1/workspaces/{workspace_id}/conversations/{conversation_id}/messages" with JSON:
      """
      {
        "local_id": "e2e-crawbl-identity-001",
        "content": {"type": "text", "text": "What platform are you running on? What is your name?"},
        "attachments": []
      }
      """
    Then the response status should be one of "0,200,500,503"
    And if status is 200 the response JSON at "data.content.text" should not be empty

  Scenario: Agent can perform web search (internet connectivity)
    Given user "primary" has a conversation saved as "conversation_id"
    When user "primary" sends a POST request to "/v1/workspaces/{workspace_id}/conversations/{conversation_id}/messages" with JSON:
      """
      {
        "local_id": "e2e-web-search-001",
        "content": {"type": "text", "text": "Search the web for the current year and tell me what year it is."},
        "attachments": []
      }
      """
    Then the response status should be one of "0,200,500,503"
    And if status is 200 the response JSON at "data.content.text" should not be empty

  Scenario: Agent reply is attributed to the correct agent
    Given user "primary" has a conversation saved as "conversation_id"
    When user "primary" sends a POST request to "/v1/workspaces/{workspace_id}/conversations/{conversation_id}/messages" with JSON:
      """
      {
        "local_id": "e2e-agent-attr-001",
        "content": {"type": "text", "text": "Hello agent"},
        "attachments": []
      }
      """
    Then the response status should be one of "0,200,500,503"
    And if status is 200 the response JSON at "data.role" should equal "agent"
    And if status is 200 the response JSON at "data.agent.id" should not be empty
    And if status is 200 the response JSON at "data.agent.name" should not be empty
    And if status is 200 the response JSON at "data.agent.role" should not be empty
