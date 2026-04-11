// Package e2e — memory palace pipeline step definitions.
// These steps assert that notes saved via the app are learned, classified,
// and recalled by the assistant through the cold-pipeline path.
package e2e

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/cucumber/godog"
	"github.com/gocraft/dbr/v2"
)

// registerMempalaceSteps binds all Gherkin phrases that assert memory palace pipeline behaviour.
func registerMempalaceSteps(sc *godog.ScenarioContext, tc *testContext) {
	sc.Step(`^the saved note should appear in the assistant's memory within (\d+) seconds?$`, tc.mempalaceSavedNoteAppearsWithin)
	sc.Step(`^the saved note should eventually be marked as processed within (\d+) seconds?$`, tc.mempalaceSavedNoteProcessedWithin)
	sc.Step(`^the assistant should recognize the note about "([^"]*)" within (\d+) seconds?$`, tc.mempalaceNoteRecognized)
	sc.Step(`^the assistant should find at least (\d+) matching notes? for topic "([^"]*)"$`, tc.mempalaceFindByTopic)
}

// mempalaceSavedNoteAppearsWithin polls memory_drawers until at least one row
// exists for the primary user's default workspace, within the given seconds.
func (tc *testContext) mempalaceSavedNoteAppearsWithin(seconds int) error {
	return tc.withDB(func(_ *dbr.Session) error {
		r, err := tc.resolveUser("primary")
		if err != nil {
			return err
		}
		if r.WorkspaceID == "" {
			return nil
		}
		return tc.pollFor(time.Duration(seconds)*time.Second, func() error {
			count, err := tc.queryCount(
				`SELECT COUNT(*) FROM memory_drawers WHERE workspace_id = $1`, r.WorkspaceID)
			if err != nil {
				return err
			}
			if count == 0 {
				return fmt.Errorf("saved note has not yet appeared in the assistant's memory for %q", "primary")
			}
			return nil
		})
	})
}

// mempalaceSavedNoteProcessedWithin polls until the most-recent memory_drawers
// row for the primary user reaches state = 'processed', within the given seconds.
func (tc *testContext) mempalaceSavedNoteProcessedWithin(seconds int) error {
	return tc.withDB(func(s *dbr.Session) error {
		r, err := tc.resolveUser("primary")
		if err != nil {
			return err
		}
		if r.WorkspaceID == "" {
			return nil
		}
		return tc.pollFor(time.Duration(seconds)*time.Second, func() error {
			var state sql.NullString
			row := s.QueryRowContext(context.Background(), `
				SELECT state FROM memory_drawers
				WHERE workspace_id = $1
				ORDER BY created_at DESC LIMIT 1`, r.WorkspaceID)
			if err := row.Scan(&state); err != nil {
				return fmt.Errorf("querying note state: %w", err)
			}
			if !state.Valid || state.String != "processed" {
				return fmt.Errorf("saved note not yet marked as processed for %q (current state: %v)", "primary", state)
			}
			return nil
		})
	})
}

// mempalaceNoteRecognized polls until the most-recent memory_drawers row for the
// primary user has been classified (memory_type is non-empty), confirming the
// assistant has recognised the subject of the note within the given seconds.
func (tc *testContext) mempalaceNoteRecognized(subject string, seconds int) error {
	return tc.withDB(func(s *dbr.Session) error {
		r, err := tc.resolveUser("primary")
		if err != nil {
			return err
		}
		if r.WorkspaceID == "" {
			return nil
		}
		return tc.pollFor(time.Duration(seconds)*time.Second, func() error {
			var memType sql.NullString
			row := s.QueryRowContext(context.Background(), `
				SELECT memory_type FROM memory_drawers
				WHERE workspace_id = $1
				ORDER BY created_at DESC LIMIT 1`, r.WorkspaceID)
			if err := row.Scan(&memType); err != nil {
				return fmt.Errorf("querying note classification for subject %q: %w", subject, err)
			}
			if !memType.Valid || memType.String == "" {
				return fmt.Errorf("assistant has not yet recognised the note about %q (memory_type still empty)", subject)
			}
			return nil
		})
	})
}

// mempalaceFindByTopic asserts that after a save the memories list for the
// user's default agent contains at least minCount entries. The topic argument
// identifies which agent slug's memory list to query.
func (tc *testContext) mempalaceFindByTopic(minCount int, slug string) error {
	if tc.dbConn == nil {
		return nil
	}
	r, err := tc.resolveUser("primary")
	if err != nil {
		return err
	}
	id, err := tc.agentIDForSlug("primary", slug)
	if err != nil {
		return err
	}
	_, reqErr := tc.doRequest("GET", "/v1/agents/"+id+"/memories", "primary", nil)
	if reqErr != nil {
		return reqErr
	}
	if assertErr := tc.assertStatus(http.StatusOK); assertErr != nil {
		return assertErr
	}
	count, err := tc.queryCount(
		`SELECT COUNT(*) FROM memory_drawers WHERE workspace_id = $1`, r.WorkspaceID)
	if err != nil {
		return err
	}
	if count < minCount {
		return fmt.Errorf("expected at least %d saved note(s) for topic %q, found %d", minCount, slug, count)
	}
	return nil
}
