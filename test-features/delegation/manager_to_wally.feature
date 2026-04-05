Feature: Manager delegates research to Wally
  As a signed-in user
  I want the Manager to hand research tasks to Wally automatically
  So the right specialist handles my request without me choosing manually

  Background:
    Given user "primary" has signed up
    And user "primary" opens their default workspace
    And user "primary" opens the swarm conversation
    And user "primary" waits until their assistant is ready

  Scenario: The Manager hands research tasks to Wally and the hand-off is recorded
    When user "primary" sends the message "Please research what programming language Kubernetes is written in and give me a brief summary." in the current conversation
    Then the assistant reply should succeed
    And the assistant reply should contain text
    And the assistant reply should come from the "wally" agent
    And the assistant should have delegated at least 1 task for subject "primary"
