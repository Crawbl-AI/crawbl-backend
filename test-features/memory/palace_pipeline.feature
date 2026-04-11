Feature: Memory palace cold pipeline
  As a signed-in user
  I want notes I save to be learned and organised by my assistant
  So the assistant knows and recalls important things about me

  Background:
    Given user "primary" has signed up
    And user "primary" opens their default workspace
    And user "primary" opens the agents in their default workspace

  Scenario: Saved notes appear in the assistant's memory
    When user "primary" saves a memory with key "cold_pipeline_note" and content "I love hiking in the mountains" for agent "wally"
    Then the saved note should appear in the assistant's memory within 90 seconds
    And the saved note should eventually be marked as processed within 90 seconds

  Scenario: Factual notes are recognized by the assistant
    When user "primary" saves a memory with key "lang_pref" and content "My favorite programming language is Go" for agent "wally"
    Then the assistant should recognize the note about "programming language" within 90 seconds

  Scenario: The assistant can find a saved note by topic
    When user "primary" saves a memory with key "topic_note" and content "I enjoy reading science fiction books" for agent "wally"
    Then the assistant should find at least 1 matching note for topic "wally"
