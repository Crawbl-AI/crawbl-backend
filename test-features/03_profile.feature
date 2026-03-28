Feature: User profile management
  As a signed-up user
  I need to view and update my profile
  So I can personalize my account

  Background:
    Given the primary test user has signed up

  Scenario: Profile has correct defaults after sign-up
    When user "primary" sends a GET request to "/v1/users/profile"
    Then the response status should be 200
    And the response JSON at "data.firebase_uid" should equal the subject of "primary"
    And the response JSON at "data.email" should equal the email of "primary"
    And the response JSON at "data.is_deleted" should equal "false"
    And the response JSON at "data.is_banned" should equal "false"
    And the response JSON at "data.subscription.code" should equal "freemium"

  Scenario: User can update profile fields
    When user "primary" sends a PATCH request to "/v1/users" with JSON:
      """
      {
        "nickname": "e2e-nick",
        "name": "E2E",
        "surname": "Tester",
        "country_code": "DE",
        "date_of_birth": "2000-01-15T00:00:00Z",
        "preferences": {
          "platform_theme": "dark",
          "platform_language": "en",
          "currency_code": "EUR"
        }
      }
      """
    Then the response status should be 204
    When user "primary" sends a GET request to "/v1/users/profile"
    Then the response status should be 200
    And the response JSON should match:
      | path                              | value |
      | data.nickname                     | e2e-nick |
      | data.name                         | E2E   |
      | data.surname                      | Tester |
      | data.country_code                 | DE    |
      | data.preferences.platform_theme    | dark  |
      | data.preferences.platform_language | en    |
      | data.preferences.currency_code     | EUR   |
    And the database user "primary" should have nickname "e2e-nick"
    And the database user "primary" should have country_code "DE"

  Scenario: Register FCM push token
    When user "primary" sends a POST request to "/v1/fcm-token" with JSON:
      """
      {"push_token": "fcm-token-e2e-device1"}
      """
    Then the response status should be 200
    And the response JSON at "success" should equal "true"
    And the database should have push token "fcm-token-e2e-device1" for subject "primary"
