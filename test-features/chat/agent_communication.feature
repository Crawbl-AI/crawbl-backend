Feature: Ready assistant replies
  As a signed-in user
  I want useful replies only after the runtime is actually ready
  So the test reflects the real product experience

  Background:
    Given user "primary" has signed up
    And user "primary" opens their default workspace
    And user "primary" opens the swarm conversation
    And user "primary" waits until their assistant is ready

  @llm-flaky
  Scenario: The assistant answers a real planning request
    When user "primary" sends the message "Name the color of the clear daytime sky in exactly one word." in the current conversation
    Then the assistant reply should succeed
    And the assistant reply should contain text
    And the assistant reply should come from an agent
    And the assistant reply should mention "blue"

  Scenario: Reply metadata identifies the responding agent
    When user "primary" sends the message "What is your role in this workspace?" in the current conversation
    Then the assistant reply should succeed
    And the assistant reply should come from an agent
