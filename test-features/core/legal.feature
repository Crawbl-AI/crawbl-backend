Feature: Legal acceptance
  As a mobile app user
  I need to review and accept the current legal documents
  Before I fully use the product

  Background:
    Given user "primary" has signed up

  Scenario: The app can show the current legal versions
    When user "primary" opens their legal status
    Then user "primary" should see the current legal versions

  Scenario: The user can accept the current legal documents
    When user "primary" accepts the current legal documents
    Then user "primary" should show accepted legal documents
