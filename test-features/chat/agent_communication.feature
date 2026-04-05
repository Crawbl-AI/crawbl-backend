Feature: Ready assistant replies
  As a signed-in user
  I want useful replies only after the runtime is actually ready
  So the test reflects the real product experience

  Background:
    Given user "primary" has signed up
    And user "primary" opens their default workspace
    And user "primary" opens the swarm conversation
    And user "primary" waits until their assistant is ready

  Scenario: The assistant answers a real planning request
    When user "primary" sends the message "Give me one short first step for starting a new project." in the current conversation
    Then the assistant reply should succeed
    And the assistant reply should contain text
    And the assistant reply should come from an agent

  Scenario: Reply metadata identifies the responding agent
    When user "primary" sends the message "What is your role in this workspace?" in the current conversation
    Then the assistant reply should succeed
    And the assistant reply should come from an agent
