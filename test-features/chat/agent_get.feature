Feature: Individual agent access
  As a signed-in user
  I want to view a single agent by its identifier
  So I can see its details without loading the full list

  Background:
    Given user "primary" has signed up
    And user "primary" opens their default workspace
    And user "primary" opens the agents in their default workspace

  Scenario: A user can retrieve a single agent's details
    When user "primary" opens the details for agent "wally"
    Then the response status should be 200
    And the response JSON at "data.name" should not be empty

  Scenario: A user can retrieve a single agent by identifier
    When user "primary" opens the agent "wally"
    Then the response status should be 200
    And the response JSON at "data.slug" should equal "wally"
    And the response JSON at "data.name" should not be empty

  Scenario: A non-existent agent returns not found
    When user "primary" sends a GET request to "/v1/agents/00000000-0000-0000-0000-000000000000"
    Then the response status should be 404
