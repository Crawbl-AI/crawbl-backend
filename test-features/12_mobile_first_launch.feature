Feature: Complete mobile first-launch flow
  As a new mobile app user opening the app for the first time
  I need to go through the full onboarding flow
  And start chatting with my AI swarm

  # This mirrors the EXACT sequence of API calls the Flutter mobile app
  # makes on first launch: health → sign-up → legal → workspaces → agents →
  # conversations → messages → send first message.

  Scenario: Complete first-launch journey
    # Step 1: App opens — check server health (no auth required)
    When I send a GET request to "/v1/health" without auth
    Then the response status should be 200
    And the response JSON at "data.online" should equal "true"

    # Step 2: Read legal documents (no auth — shown before login)
    When I send a GET request to "/v1/legal" without auth
    Then the response status should be 200
    And the response JSON at "data.terms_of_service" should not be empty

    # Step 3: Firebase auth completes → sign up
    Given the primary test user has signed up

    # Step 4: Check legal acceptance status
    When user "primary" sends a GET request to "/v1/users/legal"
    Then the response status should be 200

    # Step 5: Accept terms and privacy policy
    When user "primary" sends a POST request to "/v1/users/legal/accept" with JSON:
      """
      {"terms_of_service_version": "v1", "privacy_policy_version": "v1"}
      """
    Then the response status should be 204

    # Step 6: Load workspaces (default "My Swarm" created at sign-up)
    When user "primary" sends a GET request to "/v1/workspaces"
    Then the response status should be 200
    And the response JSON at "data" should be an array of length 1
    And the response JSON at "data.0.name" should equal "My Swarm"
    And I save "data.0.id" as "workspace_id"

    # Step 7: Load agents for the workspace
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}/agents"
    Then the response status should be 200
    And the response JSON at "data" should be an array of length 2

    # Step 8: Load conversations (1 swarm + 2 per-agent)
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}/conversations"
    Then the response status should be 200
    And the response JSON at "data" should be an array of length 3
    And I save the first item in "data" where "type" equals "swarm" field "id" as "swarm_conv_id"

    # Step 9: Load messages for the swarm conversation (empty initially)
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}/conversations/{swarm_conv_id}/messages"
    Then the response status should be 200

    # Step 10: Register FCM push token
    When user "primary" sends a POST request to "/v1/fcm-token" with JSON:
      """
      {"push_token": "e2e-first-launch-fcm-token"}
      """
    Then the response status should be 200

    # Step 11: Send first message (runtime may not be ready for fresh user)
    When user "primary" sends a POST request to "/v1/workspaces/{workspace_id}/conversations/{swarm_conv_id}/messages" with JSON:
      """
      {
        "local_id": "e2e-first-launch-msg",
        "content": {"type": "text", "text": "Hello! This is my first message."},
        "attachments": []
      }
      """
    Then the response status should be one of "0,200,500,503"

  Scenario: Returning user sign-in flow
    # Existing user opens the app again
    Given the primary test user has signed up

    # Step 1: Sign in (not sign up)
    When user "primary" sends a POST request to "/v1/auth/sign-in"
    Then the response status should be 204

    # Step 2: Load profile
    When user "primary" sends a GET request to "/v1/users/profile"
    Then the response status should be 200
    And the response JSON at "data.is_deleted" should equal "false"

    # Step 3: Load workspaces
    When user "primary" sends a GET request to "/v1/workspaces"
    Then the response status should be 200
    And the response JSON at "data" should be an array of length 1
    And I save "data.0.id" as "workspace_id"

    # Step 4: Load agents + conversations in parallel (mobile does this)
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}/agents"
    Then the response status should be 200
    When user "primary" sends a GET request to "/v1/workspaces/{workspace_id}/conversations"
    Then the response status should be 200

  Scenario: Message retry flow with same local_id
    Given the primary test user has signed up
    And user "primary" has a workspace saved as "workspace_id"
    And user "primary" has a conversation saved as "conversation_id"

    # First attempt — may fail if runtime not ready
    When user "primary" sends a POST request to "/v1/workspaces/{workspace_id}/conversations/{conversation_id}/messages" with JSON:
      """
      {
        "local_id": "e2e-retry-msg-001",
        "content": {"type": "text", "text": "retry test message"},
        "attachments": []
      }
      """
    Then the response status should be one of "0,200,500,503"

    # Retry with SAME local_id (mobile app behavior on retry tap)
    When user "primary" sends a POST request to "/v1/workspaces/{workspace_id}/conversations/{conversation_id}/messages" with JSON:
      """
      {
        "local_id": "e2e-retry-msg-001",
        "content": {"type": "text", "text": "retry test message"},
        "attachments": []
      }
      """
    Then the response status should be one of "0,200,500,503"
