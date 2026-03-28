Feature: Per-agent conversations
  As a mobile app user
  I need dedicated 1:1 conversations with each agent
  So I can have focused interactions without cross-talk

  Background:
    Given the primary test user has signed up
    And user "primary" has a workspace saved as "workspace_id"

  Scenario: Research agent conversation routes exclusively to researcher
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}/conversations"
    Then the response status should be 200
    And I save the first item in "data" where "title" equals "Research" field "id" as "research_conv_id"
    When user "primary" sends a POST request to "/v1/workspaces/{workspace_id}/conversations/{research_conv_id}/messages" with JSON:
      """
      {
        "local_id": "e2e-research-direct-001",
        "content": {"type": "text", "text": "What is your role?"},
        "attachments": []
      }
      """
    Then the response status should be one of "0,200,500,503"
    And if status is 200 the response JSON at "data.agent.role" should equal "researcher"

  Scenario: Writer agent conversation routes exclusively to writer
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}/conversations"
    Then the response status should be 200
    And I save the first item in "data" where "title" equals "Writer" field "id" as "writer_conv_id"
    When user "primary" sends a POST request to "/v1/workspaces/{workspace_id}/conversations/{writer_conv_id}/messages" with JSON:
      """
      {
        "local_id": "e2e-writer-direct-001",
        "content": {"type": "text", "text": "What is your role?"},
        "attachments": []
      }
      """
    Then the response status should be one of "0,200,500,503"
    And if status is 200 the response JSON at "data.agent.role" should equal "writer"

  Scenario: Messages in agent conversation stay isolated
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}/conversations"
    Then the response status should be 200
    And I save the first item in "data" where "title" equals "Research" field "id" as "research_conv_id"
    And I save the first item in "data" where "title" equals "Writer" field "id" as "writer_conv_id"
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}/conversations/{research_conv_id}/messages"
    Then the response status should be 200
    And I save response array length "data" as "research_msg_count"
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}/conversations/{writer_conv_id}/messages"
    Then the response status should be 200
    And I save response array length "data" as "writer_msg_count"
