// Package e2e — shared database helpers.
// Collapses subject→user→workspace resolution and related plumbing
// behind a single cached entry point so step files stop
// hand-rolling the same JOIN queries. Also houses runtime snapshot
// types, state accessors, lazy-init bootstrappers, data fetchers,
// capture helpers, and string/ID utilities shared across step files.
package e2e

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/tidwall/gjson"
)

// resolvedUser is the cached result of a single subject lookup.
// Scenario steps use it instead of rolling their own JOIN query
// against users + workspaces every time.
type resolvedUser struct {
	Alias       string
	Subject     string
	UserID      string
	WorkspaceID string
	TestUser    *testUser
}

// resolveUser returns the cached resolution for the given alias.
// On cache miss, it runs one round-trip to Postgres to look up the
// user's UUID and default workspace, caches the result, and returns
// it. Scenarios that sign up a new user should call
// invalidateResolvedUser(alias) first so the cache picks up the new
// subject on the next call.
//
// Returns an error only on hard DB failures. "User not found" maps
// to a friendly error so callers can distinguish setup bugs from
// transient failures.
func (tc *testContext) resolveUser(alias string) (*resolvedUser, error) {
	if tc.resolved == nil {
		tc.resolved = make(map[string]*resolvedUser)
	}
	if cached, ok := tc.resolved[alias]; ok && cached != nil {
		return cached, nil
	}
	subject := tc.resolveSubject(alias)
	if tc.dbConn == nil {
		r := &resolvedUser{Alias: alias, Subject: subject, TestUser: tc.users[alias]}
		tc.resolved[alias] = r
		return r, nil
	}
	sess := tc.dbConn.NewSession(nil)
	var userID, workspaceID string
	row := sess.QueryRowContext(context.Background(), `
		SELECT u.id::text, COALESCE(w.id::text, '')
		FROM users u
		LEFT JOIN workspaces w ON w.user_id = u.id
		WHERE u.subject = $1
		ORDER BY w.created_at ASC NULLS LAST
		LIMIT 1`, subject)
	if err := row.Scan(&userID, &workspaceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("resolveUser(%q): no row for subject %q", alias, subject)
		}
		return nil, fmt.Errorf("resolveUser(%q): %w", alias, err)
	}
	r := &resolvedUser{
		Alias:       alias,
		Subject:     subject,
		UserID:      userID,
		WorkspaceID: workspaceID,
		TestUser:    tc.users[alias],
	}
	tc.resolved[alias] = r
	return r, nil
}

// invalidateResolvedUser drops any cached resolution for the alias.
// Call this whenever a scenario creates, deletes, or recreates the
// backing user row so the next resolveUser call re-reads from the DB.
func (tc *testContext) invalidateResolvedUser(alias string) {
	if tc == nil || tc.resolved == nil {
		return
	}
	delete(tc.resolved, alias)
}

// withDB invokes fn with a fresh dbr session if the suite is
// configured with a --database-dsn, otherwise returns nil so steps
// without DB access still pass. This replaces the "if tc.dbConn
// == nil return nil" boilerplate at the top of assertion steps.
func (tc *testContext) withDB(fn func(s *dbr.Session) error) error {
	if tc == nil || tc.dbConn == nil {
		return nil
	}
	return fn(tc.dbConn.NewSession(nil))
}

// pollFor runs fn on a fixed interval until it returns nil or the
// given timeout expires. This replaces the 3-line
// context.WithTimeout(context.Background(), ...) + pollUntil
// boilerplate that was repeated in every polling assertion step.
func (tc *testContext) pollFor(timeout time.Duration, fn func() error) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return pollUntil(ctx, fn)
}

// pollDefault is sugar for pollFor(asyncAssertTimeout, fn). Use it
// when the step has no reason to override the default 30-second
// window.
func (tc *testContext) pollDefault(fn func() error) error {
	return tc.pollFor(asyncAssertTimeout, fn)
}

const (
	defaultRuntimeReadyTimeout = 3 * time.Minute
	defaultRuntimePollInterval = 2 * time.Second
)

// runtimeSnapshot holds a point-in-time view of the agent runtime
// status fields returned by the GET /workspaces/:id endpoint.
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

// runtimeSnapshotFromBody parses a workspace GET response body into a
// runtimeSnapshot for readiness polling.
func runtimeSnapshotFromBody(body []byte) runtimeSnapshot {
	return runtimeSnapshot{
		Status:    gjson.GetBytes(body, "data.runtime.status").String(),
		Phase:     gjson.GetBytes(body, "data.runtime.phase").String(),
		Verified:  gjson.GetBytes(body, "data.runtime.verified").Bool(),
		LastError: gjson.GetBytes(body, "data.runtime.last_error").String(),
	}
}

// runtimeReadyTimeout returns the configured timeout or the default.
func (tc *testContext) runtimeReadyTimeout() time.Duration {
	if tc.cfg != nil && tc.cfg.RuntimeReadyTimeout > 0 {
		return tc.cfg.RuntimeReadyTimeout
	}
	return defaultRuntimeReadyTimeout
}

// userState returns (or lazily creates) the per-user journey state for
// the given alias.
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

// ensureDefaultWorkspace fetches the workspace list for alias if not
// already cached in state.
func (tc *testContext) ensureDefaultWorkspace(alias string) error {
	state := tc.userState(alias)
	if state.workspaceID != "" {
		return nil
	}
	return tc.fetchWorkspaceList(alias)
}

// ensureAgentCatalog fetches the agent catalog for alias if not already
// cached in state.
func (tc *testContext) ensureAgentCatalog(alias string) error {
	state := tc.userState(alias)
	if len(state.agentIDsBySlug) > 0 {
		return nil
	}
	return tc.fetchAgents(alias)
}

// ensureConversationCatalog fetches the conversation list for alias if
// not already cached in state.
func (tc *testContext) ensureConversationCatalog(alias string) error {
	state := tc.userState(alias)
	if state.swarmConversationID != "" || len(state.conversationIDsByKey) > 0 {
		return nil
	}
	return tc.fetchConversations(alias)
}

func (tc *testContext) fetchWorkspaceList(alias string) error {
	if _, err := tc.doRequest("GET", "/v1/workspaces", alias, nil); err != nil {
		return err
	}
	if err := tc.assertStatus(http.StatusOK); err != nil {
		return err
	}
	return tc.captureDefaultWorkspace(alias)
}

func (tc *testContext) fetchAgents(alias string) error {
	if err := tc.ensureDefaultWorkspace(alias); err != nil {
		return err
	}
	state := tc.userState(alias)
	if _, err := tc.doRequest("GET", workspacesPath+state.workspaceID+"/agents", alias, nil); err != nil {
		return err
	}
	if err := tc.assertStatus(http.StatusOK); err != nil {
		return err
	}
	return tc.captureAgents(alias)
}

func (tc *testContext) fetchConversations(alias string) error {
	if err := tc.ensureDefaultWorkspace(alias); err != nil {
		return err
	}
	state := tc.userState(alias)
	if _, err := tc.doRequest("GET", workspacesPath+state.workspaceID+"/conversations", alias, nil); err != nil {
		return err
	}
	if err := tc.assertStatus(http.StatusOK); err != nil {
		return err
	}
	return tc.captureConversations(alias)
}

func (tc *testContext) captureDefaultWorkspace(alias string) error {
	state := tc.userState(alias)
	workspaceID := gjson.GetBytes(tc.lastBody, firstIDPath).String()
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

// nextLocalID generates a unique local ID for a message or attachment
// scoped to the given alias and kind.
func (tc *testContext) nextLocalID(alias, kind string) string {
	return fmt.Sprintf("e2e-%s-%s-%d", alias, kind, time.Now().UnixNano())
}

// normalizeKey lowercases and trims a string for use as a map key.
func normalizeKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
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
