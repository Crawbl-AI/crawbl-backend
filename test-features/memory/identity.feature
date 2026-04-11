Feature: Personal summary for the assistant
  As a signed-in user
  I want the assistant to know a personal summary about me
  So it can personalise every response to my background and interests

  Background:
    Given user "primary" has signed up

  Scenario: A new user has no personal summary yet
    Then the assistant should have no personal summary for "primary"

  Scenario: Setting a personal summary updates what the assistant knows
    When the assistant learns that "primary" is "a data scientist who loves hiking"
    Then the assistant's personal summary for "primary" should mention "data scientist"

  Scenario: Updating the personal summary replaces the previous one
    When the assistant learns that "primary" is "a product manager who enjoys cycling"
    And the assistant learns that "primary" is "a data scientist who loves hiking"
    Then the assistant's personal summary for "primary" should mention "data scientist"
    And the assistant's personal summary for "primary" should not mention "product manager"
