Feature: Account access
  As a mobile app user
  I need to create an account once and come back later without duplicate setup
  So I can get into my workspace quickly

  Scenario: Signing up creates the account and default workspace
    When user "primary" signs up
    Then user "primary" should exist in the database
    And user "primary" should have one workspace in the database
    And user "primary" should have a single default workspace

  Scenario: Existing user can sign in again
    Given user "primary" has signed up
    When user "primary" signs in
    Then user "primary" should see their default profile details

  Scenario: Signing up again does not duplicate the default workspace
    Given user "primary" has signed up
    When user "primary" signs up
    Then user "primary" should have one workspace in the database
