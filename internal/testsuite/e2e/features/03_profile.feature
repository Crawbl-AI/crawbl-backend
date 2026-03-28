Feature: User profile management
  As a signed-up user
  I need to view and update my profile
  So I can personalize my account

  Background:
    Given a new test user "bob"
    And user "bob" has signed up

  Scenario: Profile has correct defaults after sign-up
    When user "bob" sends a GET request to "/v1/users/profile"
    Then the response status should be 200
    And the response JSON at "data.firebase_uid" should equal the subject of "bob"
    And the response JSON at "data.email" should equal the email of "bob"
    And the response JSON at "data.is_deleted" should equal "false"
    And the response JSON at "data.is_banned" should equal "false"
    And the response JSON at "data.subscription.code" should equal "freemium"

  Scenario: User can update profile fields
    When user "bob" sends a PATCH request to "/v1/users" with JSON:
      """
      {
        "nickname": "bobby",
        "name": "Bob",
        "surname": "Tester",
        "country_code": "DE",
        "date_of_birth": "1995-06-15T00:00:00Z",
        "preferences": {
          "platform_theme": "dark",
          "platform_language": "en",
          "currency_code": "EUR"
        }
      }
      """
    Then the response status should be 204
    When user "bob" sends a GET request to "/v1/users/profile"
    Then the response status should be 200
    And the response JSON should match:
      | path                              | value |
      | data.nickname                     | bobby |
      | data.name                         | Bob   |
      | data.surname                      | Tester |
      | data.country_code                 | DE    |
      | data.preferences.platform_theme    | dark  |
      | data.preferences.platform_language | en    |
      | data.preferences.currency_code     | EUR   |
    And the database user "bob" should have nickname "bobby"
    And the database user "bob" should have country_code "DE"

  Scenario: Register FCM push token
    When user "bob" sends a POST request to "/v1/fcm-token" with JSON:
      """
      {"push_token": "fcm-token-bob-device1"}
      """
    Then the response status should be 200
    And the response JSON at "success" should equal "true"
    And the database should have push token "fcm-token-bob-device1" for subject "bob"
