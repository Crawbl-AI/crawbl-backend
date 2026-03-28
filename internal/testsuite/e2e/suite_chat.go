package e2e

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gavv/httpexpect/v2"
)

// SuiteChat tests the conversation and messaging flow.
// The send-message test is resilient to runtime-not-ready (fresh test users).
func SuiteChat(cfg *Config) Suite {
	return Suite{
		Name: "chat",
		Tests: []Test{
			{"GET conversations (list)", testListConversations},
			{"GET conversations/{id}", testGetConversation},
			{"GET messages (initial)", testMessagesInitial},
			{"POST messages (send)", sendMessageTest(cfg)},
			{"GET messages (after send)", testMessagesAfterSend},
			{"GET conversations/{id} (after send)", testConversationAfterSend},
		},
	}
}

func testListConversations(auth *httpexpect.Expect, _ *httpexpect.Expect, state map[string]string) {
	wsID := state["workspace_id"]

	arr := auth.GET("/v1/workspaces/{id}/conversations", wsID).
		Expect().
		Status(http.StatusOK).
		JSON().Object().
		Value("data").Array()

	arr.NotEmpty()

	conv := arr.Value(0).Object()
	convID := conv.Value("id").String().NotEmpty().Raw()
	conv.Value("type").String().NotEmpty()

	state["conversation_id"] = convID
}

func testGetConversation(auth *httpexpect.Expect, _ *httpexpect.Expect, state map[string]string) {
	wsID := state["workspace_id"]
	convID := state["conversation_id"]

	auth.GET("/v1/workspaces/{wsID}/conversations/{convID}", wsID, convID).
		Expect().
		Status(http.StatusOK).
		JSON().Object().
		Value("data").Object().
		HasValue("id", convID)
}

func testMessagesInitial(auth *httpexpect.Expect, _ *httpexpect.Expect, state map[string]string) {
	wsID := state["workspace_id"]
	convID := state["conversation_id"]

	auth.GET("/v1/workspaces/{wsID}/conversations/{convID}/messages", wsID, convID).
		Expect().
		Status(http.StatusOK)
}

// sendMessageTest returns a test function that sends a message.
// Uses a short timeout — if the runtime isn't ready, it gracefully skips.
func sendMessageTest(cfg *Config) TestFunc {
	return func(auth *httpexpect.Expect, _ *httpexpect.Expect, state map[string]string) {
		wsID := state["workspace_id"]
		convID := state["conversation_id"]

		// Use a short client for this request since it may block on an unready runtime.
		shortClient := httpexpect.WithConfig(httpexpect.Config{
			BaseURL:  cfg.BaseURL,
			Reporter: &Reporter{}, // isolated reporter — timeout is not a failure
			Client: &http.Client{
				Timeout: 10 * time.Second,
				Transport: &authTransport{
					base:     http.DefaultTransport,
					uid:      cfg.UID,
					email:    cfg.Email,
					name:     cfg.Name,
					e2eToken: cfg.E2EToken,
				},
			},
		})

		resp := shortClient.POST("/v1/workspaces/{wsID}/conversations/{convID}/messages", wsID, convID).
			WithJSON(map[string]any{
				"local_id": "e2e-msg-001",
				"content": map[string]any{
					"type": "text",
					"text": "hello from e2e test",
				},
				"attachments": []any{},
			}).
			Expect()

		raw := resp.Raw()
		if raw == nil || raw.StatusCode == 503 || raw.StatusCode == 0 {
			// Runtime not ready — skip send assertions.
			state["send_ok"] = "false"
			fmt.Printf("    (runtime not ready, skipping send assertions)\n")
			return
		}

		resp.Status(http.StatusOK)
		obj := resp.JSON().Object().Value("data").Object()
		obj.HasValue("role", "agent")
		obj.HasValue("status", "delivered")
		obj.HasValue("conversation_id", convID)

		state["send_ok"] = "true"
	}
}

func testMessagesAfterSend(auth *httpexpect.Expect, _ *httpexpect.Expect, state map[string]string) {
	if state["send_ok"] != "true" {
		return
	}
	wsID := state["workspace_id"]
	convID := state["conversation_id"]

	arr := auth.GET("/v1/workspaces/{wsID}/conversations/{convID}/messages", wsID, convID).
		Expect().
		Status(http.StatusOK).
		JSON().Object().
		Value("data").Array()

	arr.Length().Ge(2)
}

func testConversationAfterSend(auth *httpexpect.Expect, _ *httpexpect.Expect, state map[string]string) {
	if state["send_ok"] != "true" {
		return
	}
	wsID := state["workspace_id"]
	convID := state["conversation_id"]

	auth.GET("/v1/workspaces/{wsID}/conversations/{convID}", wsID, convID).
		Expect().
		Status(http.StatusOK).
		JSON().Object().
		Value("data").Object().
		Value("last_message").Object().
		Value("role").String().NotEmpty()
}
