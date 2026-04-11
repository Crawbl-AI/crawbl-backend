Feature: Assistant tool use is recorded
  As a platform operator
  I want every tool call made by the assistant to be captured
  So I can audit what the assistant did on behalf of each user

  Background:
    Given user "primary" has signed up
    And user "primary" opens their default workspace
    And user "primary" opens the swarm conversation
    And user "primary" waits until their assistant is ready

  @llm-flaky
  Scenario: Successful tool use is recorded
    When user "primary" mentions the "wally" agent in the swarm conversation saying "Please save a note to your long-term memory with key note_audit and content: This is a test note."
    Then the assistant reply should succeed
    And the assistant reply should contain text
    And the assistant's save-memory tool use should be recorded as successful within 10 seconds
    And the save-memory tool use should have taken a measurable amount of time

  @llm-flaky
  Scenario: All tool use records share a session
    When user "primary" mentions the "wally" agent in the swarm conversation saying "Please save a note to your long-term memory with key session_check_1 and content: first note."
    Then the assistant reply should succeed
    When user "primary" mentions the "wally" agent in the swarm conversation saying "Please save another note to your long-term memory with key session_check_2 and content: second note."
    Then the assistant reply should succeed
    And the assistant's recent tool uses should belong to the same session
