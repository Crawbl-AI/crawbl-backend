// Package e2e — shared helpers used across all step definition files.
//
// This file contains testContext methods that are not Gherkin steps
// themselves but are called by multiple step files (workspace
// bootstrapping, agent/conversation capture, message sending,
// runtime polling, string normalisation). Keeping them here avoids
// import cycles and keeps each steps_*.go focused on registration +
// domain logic.
package e2e

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

const (
	defaultRuntimeReadyTimeout = 3 * time.Minute
	defaultRuntimePollInterval = 2 * time.Second

	// maxBodyDisplayLen is the maximum number of characters shown when
	// truncating a response body in error messages.
	maxBodyDisplayLen = 200

	// asyncAssertTimeout is how long polling assertions wait for
	// async agent-side effects (memory, audit, delegation, etc.).
	asyncAssertTimeout = 30 * time.Second

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

	// HTTP status code constants used by assertion steps.
	statusOK                 = 200
	statusCreated            = 201
	statusNoContent          = 204
	statusBadRequest         = 400
	statusUnauthorized       = 401
	statusNotFound           = 404
	statusServiceUnavailable = 503
)

type runtimeSnapshot struct {
	Status    string
	Phase     string
	Verified  bool
	LastError string
}

func (s runtimeSnapshot) ready() bool {
	return s.Verified && (strings.EqualFold(s.Status, "ready") || strings.EqualFold(s.Phase, "ready"))
}

func (s runtimeSnapshot) failed() bool {
	return strings.EqualFold(s.Status, "failed") || strings.EqualFold(s.Phase, "error")
}

// --- State accessors -------------------------------------------------

func (tc *testContext) userState(alias string) *userJourneyState {
	state, ok := tc.state[alias]
	if ok {
		return state
	}
	state = &userJourneyState{
		agentIDsBySlug:       make(map[string]string),
		agentNamesBySlug:     make(map[string]string),
		conversationIDsByKey: make(map[string]string),
	}
	tc.state[alias] = state
	return state
}

// --- Lazy-init bootstrappers ----------------------------------------

func (tc *testContext) ensureDefaultWorkspace(alias string) error {
	state := tc.userState(alias)
	if state.workspaceID != "" {
		return nil
	}
	return tc.fetchWorkspaceList(alias)
}

func (tc *testContext) ensureAgentCatalog(alias string) error {
	state := tc.userState(alias)
	if len(state.agentIDsBySlug) > 0 {
		return nil
	}
	return tc.fetchAgents(alias)
}

func (tc *testContext) ensureConversationCatalog(alias string) error {
	state := tc.userState(alias)
	if state.swarmConversationID != "" || len(state.conversationIDsByKey) > 0 {
		return nil
	}
	return tc.fetchConversations(alias)
}

// --- Data fetchers (call API + capture into state) -------------------

func (tc *testContext) fetchWorkspaceList(alias string) error {
	if _, err := tc.doRequest("GET", "/v1/workspaces", alias, nil); err != nil {
		return err
	}
	if err := tc.assertStatus(statusOK); err != nil {
		return err
	}
	return tc.captureDefaultWorkspace(alias)
}

func (tc *testContext) fetchAgents(alias string) error {
	if err := tc.ensureDefaultWorkspace(alias); err != nil {
		return err
	}
	state := tc.userState(alias)
	if _, err := tc.doRequest("GET", "/v1/workspaces/"+state.workspaceID+"/agents", alias, nil); err != nil {
		return err
	}
	if err := tc.assertStatus(statusOK); err != nil {
		return err
	}
	return tc.captureAgents(alias)
}

func (tc *testContext) fetchConversations(alias string) error {
	if err := tc.ensureDefaultWorkspace(alias); err != nil {
		return err
	}
	state := tc.userState(alias)
	if _, err := tc.doRequest("GET", "/v1/workspaces/"+state.workspaceID+"/conversations", alias, nil); err != nil {
		return err
	}
	if err := tc.assertStatus(statusOK); err != nil {
		return err
	}
	return tc.captureConversations(alias)
}

// --- Capture helpers -------------------------------------------------

func (tc *testContext) captureDefaultWorkspace(alias string) error {
	state := tc.userState(alias)
	workspaceID := gjson.GetBytes(tc.lastBody, "data.0.id").String()
	if workspaceID == "" {
		return fmt.Errorf("no default workspace found for %q", alias)
	}
	state.workspaceID = workspaceID
	state.workspaceName = gjson.GetBytes(tc.lastBody, "data.0.name").String()
	// Expose to generic HTTP steps so {workspace_id} interpolation works.
	tc.saved["workspace_id"] = workspaceID
	return nil
}

func (tc *testContext) captureAgents(alias string) error {
	state := tc.userState(alias)
	for _, item := range gjson.GetBytes(tc.lastBody, "data").Array() {
		slug := normalizeKey(item.Get("slug").String())
		if slug == "" {
			continue
		}
		state.agentIDsBySlug[slug] = item.Get("id").String()
		state.agentNamesBySlug[slug] = item.Get("name").String()
	}
	if len(state.agentIDsBySlug) == 0 {
		return fmt.Errorf("no agents returned for %q", alias)
	}
	return nil
}

func (tc *testContext) captureConversations(alias string) error {
	state := tc.userState(alias)
	for _, item := range gjson.GetBytes(tc.lastBody, "data").Array() {
		convID := item.Get("id").String()
		if convID == "" {
			continue
		}
		convType := normalizeKey(item.Get("type").String())
		titleKey := normalizeKey(item.Get("title").String())
		agentSlug := normalizeKey(item.Get("agent.slug").String())
		if convType == "swarm" {
			state.swarmConversationID = convID
			state.conversationIDsByKey["swarm"] = convID
		}
		if titleKey != "" {
			state.conversationIDsByKey[titleKey] = convID
		}
		if agentSlug != "" {
			state.conversationIDsByKey[agentSlug] = convID
		}
	}
	if state.swarmConversationID == "" {
		return fmt.Errorf("no swarm conversation returned for %q", alias)
	}
	return nil
}

// --- Message helpers -------------------------------------------------

// sendMessage POSTs a user message and then polls the conversation
// messages endpoint until at least one assistant reply has arrived.
// The backend's POST /messages handler returns 201 with the user
// message immediately and streams the assistant reply over Socket.IO,
// so this helper closes the loop for scenarios that still expect the
// old "HTTP-blocking until assistant reply" shape — it rewrites
// tc.lastBody to {"data": [assistantReplies...]} so the downstream
// "assistant reply should ..." step definitions keep working.
//
// Empty text still triggers the 400 validation path; those scenarios
// assert on the POST response directly and never need the poll stage.
func (tc *testContext) sendMessage(alias, text string) error {
	state := tc.userState(alias)
	if state.currentConversation == "" {
		return fmt.Errorf("no current conversation set for %q — open one first", alias)
	}
	body := map[string]any{
		"local_id":    tc.nextLocalID(alias, "message"),
		"content":     map[string]any{"type": "text", "text": text},
		"attachments": []any{},
	}
	if _, err := tc.doRequest("POST", "/v1/workspaces/"+state.workspaceID+"/conversations/"+state.currentConversation+"/messages", alias, body); err != nil {
		return err
	}
	// Empty-text scenarios expect a rejection status (400/422/etc.);
	// leave the response untouched so assertion steps can check it.
	if strings.TrimSpace(text) == "" {
		return nil
	}
	// Validation or other hard errors on the send path should surface
	// immediately; we only enter the polling phase after a create.
	if tc.lastStatus != statusOK && tc.lastStatus != statusCreated {
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
	listURL := "/v1/workspaces/" + state.workspaceID + "/conversations/" + state.currentConversation + "/messages"
	deadline := time.Now().Add(assistantReplyPollWindow)
	for {
		if _, err := tc.doRequest("GET", listURL, alias, nil); err != nil {
			return fmt.Errorf("send: poll messages: %w", err)
		}
		if tc.lastStatus != statusOK {
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
			tc.lastStatus = statusOK
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

func (tc *testContext) sendWarmupMessage(alias string) error {
	state := tc.userState(alias)
	body := map[string]any{
		"local_id":    tc.nextLocalID(alias, "warmup"),
		"content":     map[string]any{"type": "text", "text": "Reply with the single word READY."},
		"attachments": []any{},
	}
	_, err := tc.doRequest("POST", "/v1/workspaces/"+state.workspaceID+"/conversations/"+state.currentConversation+"/messages", alias, body)
	return err
}

// --- Utility functions -----------------------------------------------

func (tc *testContext) nextLocalID(alias, kind string) string {
	return fmt.Sprintf("e2e-%s-%s-%d", alias, kind, time.Now().UnixNano())
}

func (tc *testContext) runtimeReadyTimeout() time.Duration {
	if tc.cfg != nil && tc.cfg.RuntimeReadyTimeout > 0 {
		return tc.cfg.RuntimeReadyTimeout
	}
	return defaultRuntimeReadyTimeout
}

func (tc *testContext) runtimePollInterval() time.Duration {
	if tc.cfg != nil && tc.cfg.RuntimePollInterval > 0 {
		return tc.cfg.RuntimePollInterval
	}
	return defaultRuntimePollInterval
}

func runtimeSnapshotFromBody(body []byte) runtimeSnapshot {
	return runtimeSnapshot{
		Status:    gjson.GetBytes(body, "data.runtime.status").String(),
		Phase:     gjson.GetBytes(body, "data.runtime.phase").String(),
		Verified:  gjson.GetBytes(body, "data.runtime.verified").Bool(),
		LastError: gjson.GetBytes(body, "data.runtime.last_error").String(),
	}
}

func normalizeKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func abbreviatedBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > maxBodyDisplayLen {
		return text[:maxBodyDisplayLen]
	}
	return text
}

// agentIDForSlug resolves a human slug ("wally") to the agent UUID,
// fetching the agent catalog lazily if it hasn't been loaded yet.
func (tc *testContext) agentIDForSlug(alias, slug string) (string, error) {
	if err := tc.ensureAgentCatalog(alias); err != nil {
		return "", err
	}
	state := tc.userState(alias)
	id := state.agentIDsBySlug[normalizeKey(slug)]
	if id == "" {
		return "", fmt.Errorf("no agent with slug %q for user %q", slug, alias)
	}
	return id, nil
}
