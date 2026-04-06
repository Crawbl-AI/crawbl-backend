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
	"fmt"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

const (
	defaultRuntimeReadyTimeout = 3 * time.Minute
	defaultRuntimePollInterval = 2 * time.Second
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
	if err := tc.assertStatus(200); err != nil {
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
	if err := tc.assertStatus(200); err != nil {
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
	if err := tc.assertStatus(200); err != nil {
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

func (tc *testContext) sendMessage(alias, text string) error {
	state := tc.userState(alias)
	if state.currentConversation == "" {
		return fmt.Errorf("no current conversation set for %q — open one first", alias)
	}
	body := map[string]any{
		"local_id": tc.nextLocalID(alias, "message"),
		"content":  map[string]any{"type": "text", "text": text},
		"attachments": []any{},
	}
	_, err := tc.doRequest("POST", "/v1/workspaces/"+state.workspaceID+"/conversations/"+state.currentConversation+"/messages", alias, body)
	return err
}

func (tc *testContext) sendWarmupMessage(alias string) error {
	state := tc.userState(alias)
	body := map[string]any{
		"local_id": tc.nextLocalID(alias, "warmup"),
		"content":  map[string]any{"type": "text", "text": "Reply with the single word READY."},
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
	if len(text) > 200 {
		return text[:200]
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
