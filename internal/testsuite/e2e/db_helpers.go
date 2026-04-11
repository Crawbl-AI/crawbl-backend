// Package e2e — shared database helpers.
// Collapses subject→user→workspace resolution and related plumbing
// behind a single cached entry point so step files stop
// hand-rolling the same JOIN queries.
package e2e

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
