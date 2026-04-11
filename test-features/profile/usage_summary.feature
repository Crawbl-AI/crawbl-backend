Feature: User monthly usage summary
  As a signed-in user
  I want to see how much of my monthly assistant budget I have used
  So I know whether I am close to my plan limit

  Background:
    Given user "primary" has signed up

  Scenario: A new user can view their monthly usage summary
    When user "primary" opens their monthly usage summary
    Then the response status should be 200
    And the response JSON at "data.tokens_used" should not be empty
