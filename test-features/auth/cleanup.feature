Feature: Account deletion
  As a user leaving the product
  I want my account to be marked deleted and treated differently afterwards
  So cleanup is deliberate and visible

  Scenario: A user can delete their account
    Given an extra test user "frank"
    And user "frank" has signed up
    When user "frank" deletes their account
    Then user "frank" should be marked as deleted in the database

  Scenario: A deleted account can no longer access its profile
    Given an extra test user "frank"
    And user "frank" has signed up
    And user "frank" has deleted their account
    When user "frank" opens their profile
    Then the response status should be 403
    And the response JSON at "code" should equal "USR0001"
