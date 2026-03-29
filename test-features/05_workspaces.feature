Feature: Default workspace setup
  As a newly signed-up user
  I want a workspace that already has the basics prepared
  So I can start using the product without manual setup

  Background:
    Given user "primary" has signed up

  Scenario: The user sees a single default workspace
    When user "primary" opens their workspace list
    Then user "primary" should have a single default workspace

  Scenario: The default workspace exposes runtime details
    When user "primary" opens their default workspace
    Then user "primary" should see runtime details for their default workspace

  Scenario: The default workspace includes the built-in agents
    When user "primary" opens the agents in their default workspace
    Then user "primary" should see the default agents

  Scenario: The default workspace includes swarm and direct conversations
    When user "primary" opens the conversations in their default workspace
    Then user "primary" should see the default conversations
