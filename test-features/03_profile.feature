Feature: Profile management
  As a signed-in user
  I want my account details and device settings to be stored correctly
  So the app feels personal and works across sessions

  Background:
    Given user "primary" has signed up

  Scenario: A new account shows sensible default profile details
    Then user "primary" should see their default profile details

  Scenario: A user can update their profile and preferences
    When user "primary" updates their profile details
    Then user "primary" should see their updated profile details

  Scenario: A device push token is stored for notifications
    When user "primary" registers a push token
    Then the push token for user "primary" should be stored
