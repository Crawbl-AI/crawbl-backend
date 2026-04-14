// Package e2e — HTTP POST+poll helpers that bridge the old HTTP-blocking
// reply shape with the current Socket.IO streaming contract.
//
// sendMessage posts a user message and then polls the conversation
// messages endpoint until the assistant reply surfaces. The result is
// rewritten into tc.lastBody so downstream assertion steps keep working
// against the familiar {"data": [...]} envelope.
package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	mobilev1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mobile/v1"
	"github.com/tidwall/gjson"
)

const (
	// assistantReplyPollWindow bounds how long sendMessage waits for
	// the assistant's first reply to surface in the conversation's
	// messages list after the POST returns 201. 3 minutes covers the
	// longest warm-runtime reply budget plus ADK tool-call latency;
	// cold-start is already gated by the warmup step.
	assistantReplyPollWindow = 3 * time.Minute

	// assistantReplyPollInterval is how often sendMessage re-checks
	// the conversation's messages list while waiting for a reply.
	// 1 second balances responsiveness against needless load.
	assistantReplyPollInterval = 1 * time.Second
)

// sendMessage POSTs a user message and then polls the conversation
// messages endpoint until at least one assistant reply has arrived.
// The backend's POST /messages handler returns 201 with the user
// message immediately and streams the assistant reply over Socket.IO,
// so this helper closes the loop for scenarios that still expect the
// old "HTTP-blocking until assistant reply" shape — it rewrites
// tc.lastBody to {"data": [assistantReplies...]} so the downstream
// "assistant reply should ..." step definitions keep working against
// the first assistant turn.
//
// Empty text still triggers the 400 validation path; those scenarios
// assert on the POST response directly and never need the poll stage.
func (tc *testContext) sendMessage(alias, text string) error {
	state := tc.userState(alias)
	if state.currentConversation == "" {
		return fmt.Errorf("no current conversation set for %q — open one first", alias)
	}
	body := &mobilev1.SendMessageRequest{
		LocalId: tc.nextLocalID(alias, "message"),
		Content: &mobilev1.MessageContentPayload{Type: "text", Text: text},
	}
	if _, err := tc.doProtoRequest("POST", pathWorkspaces+state.workspaceID+pathConversations+state.currentConversation+pathMessages, alias, body); err != nil {
		return err
	}
	// Empty-text scenarios expect a rejection status (400/422/etc.);
	// leave the response untouched so assertion steps can check it.
	if strings.TrimSpace(text) == "" {
		return nil
	}
	// Validation or other hard errors on the send path should surface
	// immediately; we only enter the polling phase after a create.
	if tc.lastStatus != http.StatusOK && tc.lastStatus != http.StatusCreated {
		return nil
	}
	userMsgID := gjson.GetBytes(tc.lastBody, "data.id").String()
	userMsgCreated := gjson.GetBytes(tc.lastBody, "data.created_at").String()
	if userMsgID == "" {
		return fmt.Errorf("send: response body missing data.id: %s", abbreviatedBody(tc.lastBody))
	}
	return tc.pollForAssistantReply(alias, userMsgID, userMsgCreated)
}

// pollForAssistantReply repeatedly lists messages in the current
// conversation until at least one message with a non-"user" role
// appears after the given user message. On success it replaces
// tc.lastBody with a synthesized {"data": [assistantReplies...]}
// envelope (oldest-first) so existing "data.0.content.text" style
// assertions keep working against the first assistant turn.
func (tc *testContext) pollForAssistantReply(alias, userMsgID, userMsgCreatedAt string) error {
	state := tc.userState(alias)
	listURL := pathWorkspaces + state.workspaceID + pathConversations + state.currentConversation + pathMessages
	deadline := time.Now().Add(assistantReplyPollWindow)
	for {
		if _, err := tc.doRequest("GET", listURL, alias, nil); err != nil {
			return fmt.Errorf("send: poll messages: %w", err)
		}
		if tc.lastStatus != http.StatusOK {
			return fmt.Errorf("send: list messages returned %d; body: %s", tc.lastStatus, abbreviatedBody(tc.lastBody))
		}
		// GET /messages returns {"data": {"messages": [...]}}.
		msgs := gjson.GetBytes(tc.lastBody, "data.messages").Array()
		replies := collectAssistantRepliesAfter(msgs, userMsgID, userMsgCreatedAt)
		if len(replies) > 0 {
			synthesized, err := synthesizeRepliesBody(replies)
			if err != nil {
				return fmt.Errorf("send: synthesize replies: %w", err)
			}
			tc.lastBody = synthesized
			tc.lastStatus = http.StatusOK
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("send: assistant did not reply within %s (user msg %s)", assistantReplyPollWindow, userMsgID)
		}
		time.Sleep(assistantReplyPollInterval)
	}
}

// collectAssistantRepliesAfter filters a message list to those with a
// non-"user" role that arrived strictly after the anchor user message.
// Ordering is preserved from the backend's list endpoint, which is
// ascending by created_at, so the zero-indexed reply is the first
// turn the assistant produced.
func collectAssistantRepliesAfter(msgs []gjson.Result, anchorID, anchorCreated string) []gjson.Result {
	var out []gjson.Result
	anchorSeen := false
	for _, m := range msgs {
		if m.Get("id").String() == anchorID {
			anchorSeen = true
			continue
		}
		if !anchorSeen && anchorCreated != "" && m.Get("created_at").String() <= anchorCreated {
			continue
		}
		role := m.Get("role").String()
		if role == "" || role == "user" {
			continue
		}
		out = append(out, m)
	}
	return out
}

// synthesizeRepliesBody wraps the given gjson results in a
// {"data": [...]} envelope so older "data.0.content.text" assertions
// keep working after the backend moved to a 201+stream shape.
func synthesizeRepliesBody(replies []gjson.Result) ([]byte, error) {
	raws := make([]json.RawMessage, 0, len(replies))
	for _, r := range replies {
		raws = append(raws, json.RawMessage(r.Raw))
	}
	return json.Marshal(map[string]any{"data": raws})
}

// sendWarmupMessage sends a minimal probe message used by the warmup
// step to confirm the agent runtime is accepting requests before the
// main scenario begins.
func (tc *testContext) sendWarmupMessage(alias string) error {
	state := tc.userState(alias)
	body := &mobilev1.SendMessageRequest{
		LocalId: tc.nextLocalID(alias, "warmup"),
		Content: &mobilev1.MessageContentPayload{Type: "text", Text: "Reply with the single word READY."},
	}
	_, err := tc.doProtoRequest("POST", pathWorkspaces+state.workspaceID+pathConversations+state.currentConversation+pathMessages, alias, body)
	return err
}
