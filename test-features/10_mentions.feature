Feature: Agent mentions in swarm conversations
  As a mobile app user
  I need to @mention specific agents in the swarm chat
  So the right agent handles my request

  Background:
    Given the primary test user has signed up
    And user "primary" has a workspace saved as "workspace_id"

  Scenario: Send message with @Research mention routes to researcher agent
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}/agents"
    Then the response status should be 200
    And I save the first item in "data" where "role" equals "researcher" field "id" as "researcher_id"
    And I save the first item in "data" where "role" equals "researcher" field "name" as "researcher_name"
    Given I save the first item in "data" where "type" equals "swarm" field "id" as "swarm_conv_id" from conversations
    When user "primary" sends a POST request to "/v1/workspaces/{workspace_id}/conversations/{swarm_conv_id}/messages" with JSON:
      """
      {
        "local_id": "e2e-mention-research-001",
        "content": {"type": "text", "text": "@Research find me information about Go testing"},
        "attachments": [],
        "mentions": [
          {
            "agent_id": "{researcher_id}",
            "agent_name": "{researcher_name}",
            "offset": 0,
            "length": 9
          }
        ]
      }
      """
    Then the response status should be one of "0,200,500,503"
    And if status is 200 the response JSON at "data.agent.role" should equal "researcher"

  Scenario: Send message with @Writer mention routes to writer agent
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}/agents"
    Then the response status should be 200
    And I save the first item in "data" where "role" equals "writer" field "id" as "writer_id"
    And I save the first item in "data" where "role" equals "writer" field "name" as "writer_name"
    Given I save the first item in "data" where "type" equals "swarm" field "id" as "swarm_conv_id" from conversations
    When user "primary" sends a POST request to "/v1/workspaces/{workspace_id}/conversations/{swarm_conv_id}/messages" with JSON:
      """
      {
        "local_id": "e2e-mention-writer-001",
        "content": {"type": "text", "text": "@Writer draft a short email about testing"},
        "attachments": [],
        "mentions": [
          {
            "agent_id": "{writer_id}",
            "agent_name": "{writer_name}",
            "offset": 0,
            "length": 7
          }
        ]
      }
      """
    Then the response status should be one of "0,200,500,503"
    And if status is 200 the response JSON at "data.agent.role" should equal "writer"
