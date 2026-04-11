Feature: Real-time reply streaming

  As a user of the Crawbl assistant
  I want to receive the assistant's reply progressively as it is generated
  So that I can read the response before it is fully complete

  Background:
    Given user "primary" has signed up
    And user "primary" has a workspace

  @wip
  Scenario: The assistant streams its reply to the user
    Given user "primary" is connected to the live update channel
    When user "primary" sends the message "Tell me a short story about a brave knight"
    Then the assistant should stream at least 1 text chunk to the user within 30 seconds
    And a final complete message should arrive for the reply within 30 seconds

  @wip @llm-flaky
  Scenario: The assistant's tool use is visible during the reply
    Given user "primary" is connected to the live update channel
    When user "primary" sends the message "Please save a note that says: streaming test note"
    Then at least one tool activity event should be received for the reply within 30 seconds
