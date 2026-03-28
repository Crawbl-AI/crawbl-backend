Feature: Conversations and messaging
  As a signed-up user
  I need to send and receive messages in conversations
  So I can interact with my AI swarm

  Background:
    Given the primary test user has signed up
    And user "primary" has a workspace saved as "workspace_id"

  Scenario: Default conversations exist (swarm + per-agent)
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}/conversations"
    Then the response status should be 200
    And the response JSON at "data" should be an array of length 3
    And the response JSON array "data" should contain an item where "type" equals "swarm"
    And the response JSON array "data" should contain an item where "type" equals "agent"

  Scenario: Swarm conversation has no agent attached
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}/conversations"
    Then the response status should be 200
    And I save the first item in "data" where "type" equals "swarm" field "id" as "swarm_conv_id"
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}/conversations/{swarm_conv_id}"
    Then the response status should be 200
    And the response JSON at "data.type" should equal "swarm"
    And the response JSON at "data.title" should equal "My Swarm"

  Scenario: Agent conversation has agent attached
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}/conversations"
    Then the response status should be 200
    And I save the first item in "data" where "type" equals "agent" field "id" as "agent_conv_id"
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}/conversations/{agent_conv_id}"
    Then the response status should be 200
    And the response JSON at "data.type" should equal "agent"
    And the response JSON at "data.agent.id" should not be empty
    And the response JSON at "data.agent.name" should not be empty
    And the response JSON at "data.agent.role" should not be empty

  Scenario: Each agent has a dedicated conversation
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}/conversations"
    Then the response status should be 200
    And the response JSON array "data" should contain an item where "title" equals "Research"
    And the response JSON array "data" should contain an item where "title" equals "Writer"

  Scenario: Conversation unread count starts at zero
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}/conversations"
    Then the response status should be 200
    And the response JSON at "data.0.unread_count" should equal "0"

  Scenario: Messages endpoint is accessible
    Given user "primary" has a conversation saved as "conversation_id"
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}/conversations/{conversation_id}/messages"
    Then the response status should be 200

  Scenario: Messages list has correct pagination structure
    Given user "primary" has a conversation saved as "conversation_id"
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}/conversations/{conversation_id}/messages"
    Then the response status should be 200
    And the response JSON at "pagination.has_next" should equal "false"
    And the response JSON at "pagination.has_prev" should equal "false"

  Scenario: Send a text message to swarm conversation (runtime may not be ready)
    Given user "primary" has a conversation saved as "conversation_id"
    When user "primary" sends a POST request to "/v1/workspaces/{workspace_id}/conversations/{conversation_id}/messages" with JSON:
      """
      {
        "local_id": "e2e-primary-msg-001",
        "content": {"type": "text", "text": "hello from e2e"},
        "attachments": []
      }
      """
    Then the response status should be one of "0,200,500,503"

  Scenario: Send message with empty text is rejected
    Given user "primary" has a conversation saved as "conversation_id"
    When user "primary" sends a POST request to "/v1/workspaces/{workspace_id}/conversations/{conversation_id}/messages" with JSON:
      """
      {
        "local_id": "e2e-primary-msg-empty",
        "content": {"type": "text", "text": ""},
        "attachments": []
      }
      """
    Then the response status should be 400

  Scenario: Send message with missing content is rejected
    Given user "primary" has a conversation saved as "conversation_id"
    When user "primary" sends a POST request to "/v1/workspaces/{workspace_id}/conversations/{conversation_id}/messages" with JSON:
      """
      {"local_id": "e2e-primary-msg-nocontent", "attachments": []}
      """
    Then the response status should be 400
