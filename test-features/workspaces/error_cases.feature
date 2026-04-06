Feature: Workspace error handling
  As a signed-in user
  I want clear errors when I access workspaces incorrectly
  So I understand what went wrong

  Background:
    Given user "primary" has signed up

  Scenario: Accessing a non-existent workspace returns not found
    When user "primary" sends a GET request to "/v1/workspaces/00000000-0000-0000-0000-000000000000"
    Then the response status should be 404

  Scenario: Accessing agents in a non-existent workspace returns not found
    When user "primary" sends a GET request to "/v1/workspaces/00000000-0000-0000-0000-000000000000/agents"
    Then the response status should be 404

  Scenario: Accessing conversations in a non-existent workspace returns not found
    When user "primary" sends a GET request to "/v1/workspaces/00000000-0000-0000-0000-000000000000/conversations"
    Then the response status should be 404
