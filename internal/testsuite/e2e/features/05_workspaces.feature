Feature: Workspace management
  As a signed-up user
  I need to view my workspaces and their agents
  So I can start using the swarm

  Background:
    Given the primary test user has signed up

  Scenario: Default workspace is created on sign-up
    When user "primary" sends a GET request to "/v1/workspaces"
    Then the response status should be 200
    And the response JSON at "data" should be an array of length 1
    And the response JSON at "data.0.name" should equal "My Swarm"
    And the response JSON at "data.0.runtime.status" should not be empty
    And I save "data.0.id" as "workspace_id"

  Scenario: Get single workspace by ID
    Given user "primary" has a workspace saved as "workspace_id"
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}"
    Then the response status should be 200
    And the response JSON at "data.id" should equal the saved "workspace_id"
    And the response JSON at "data.name" should equal "My Swarm"

  Scenario: Workspace has default agents
    Given user "primary" has a workspace saved as "workspace_id"
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}/agents"
    Then the response status should be 200
    And the response JSON at "data" should be an array of length 2
    And the response JSON array "data" should contain an item where "role" equals "researcher"
    And the response JSON array "data" should contain an item where "role" equals "writer"
