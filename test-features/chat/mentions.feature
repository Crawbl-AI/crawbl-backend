Feature: Agent mentions in the swarm chat
  As a mobile app user
  I want to pull a specific specialist into the swarm conversation
  So the right assistant handles the request

  Background:
    Given user "primary" has signed up
    And user "primary" opens their default workspace
    And user "primary" waits until their assistant is ready

  Scenario: Mentioning Wally routes the reply to Wally
    When user "primary" mentions the "wally" agent in the swarm conversation saying "Say you are ready in four words."
    Then the assistant reply should succeed
    And the assistant reply should contain text
    And the assistant reply should come from the "wally" agent
