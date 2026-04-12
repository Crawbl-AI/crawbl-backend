// Package e2e — agent configuration (blueprint) step definitions.
// These steps verify that changes to an agent's model or tool list are
// persisted and immediately visible through the user-facing settings API.
//
// Update path: no PUT /v1/agents/{id}/settings endpoint exists, so model
// and tool changes are applied directly to the agent_settings table via
// tc.dbConn. Each scenario's AfterScenario hook restores the original
// values so other tests are unaffected.
package e2e

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"github.com/tidwall/gjson"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

// blueprintState holds per-scenario original values for teardown.
type blueprintState struct {
	agentID       string
	originalModel string
	originalTools []string
}

// blueprintScenarioState is set once per scenario and cleared in AfterScenario.
var _ = (*blueprintState)(nil) // compile-time nil-pointer check

func registerBlueprintSteps(sc *godog.ScenarioContext, tc *testContext) {
	// Track original settings for teardown.
	var saved *blueprintState

	sc.Step(`^user "([^"]*)" sets the model for agent "([^"]*)" to "([^"]*)"$`, func(alias, slug, model string) error {
		s, err := tc.blueprintSetAgentModel(alias, slug, model)
		if err != nil {
			return err
		}
		saved = s
		return nil
	})

	sc.Step(`^the assistant's current configuration for agent "([^"]*)" should report model "([^"]*)"$`,
		tc.blueprintAssertAgentModel)

	sc.Step(`^user "([^"]*)" adds tool "([^"]*)" to agent "([^"]*)"$`, func(alias, tool, slug string) error {
		s, err := tc.blueprintAddAgentTool(alias, tool, slug)
		if err != nil {
			return err
		}
		if saved == nil {
			saved = s
		} else {
			// Merge — same agent, both fields may have been written.
			saved.originalTools = s.originalTools
		}
		return nil
	})

	sc.Step(`^the assistant's current tool list for agent "([^"]*)" should include "([^"]*)"$`,
		tc.blueprintAssertAgentHasTool)

	// Restore original agent settings after each scenario so the suite
	// state stays clean for subsequent scenarios.
	sc.After(func(ctx context.Context, scenario *godog.Scenario, err error) (context.Context, error) {
		if saved == nil || tc.dbConn == nil {
			saved = nil
			return ctx, nil
		}
		s := tc.sess()
		// Restore the model and tools that were present before this scenario ran.
		tools := database.StringArray(saved.originalTools)
		_, execErr := s.ExecContext(ctx, `
			UPDATE agent_settings
			SET    model        = $1,
			       allowed_tools = $2,
			       updated_at   = $3
			WHERE  agent_id = $4`,
			saved.originalModel, tools, time.Now().UTC(), saved.agentID,
		)
		if execErr != nil {
			// Log but do not fail — teardown errors must not mask scenario failures.
			_ = execErr
		}
		saved = nil
		return ctx, nil
	})
}

// blueprintSetAgentModel writes a new model value for the given agent directly
// into agent_settings. Returns a blueprintState populated with the pre-change
// values so the AfterScenario hook can restore them.
func (tc *testContext) blueprintSetAgentModel(alias, slug, model string) (*blueprintState, error) {
	if err := tc.ensureDefaultWorkspace(alias); err != nil {
		return nil, err
	}
	agentID, err := tc.agentIDForSlug(alias, slug)
	if err != nil {
		return nil, err
	}

	// Snapshot original values before writing so teardown can restore them.
	orig := tc.blueprintSnapshot(agentID)

	if tc.dbConn != nil {
		s := tc.sess()
		_, execErr := s.ExecContext(context.Background(), `
			INSERT INTO agent_settings (agent_id, model, allowed_tools, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (agent_id) DO UPDATE
			SET model = EXCLUDED.model, updated_at = EXCLUDED.updated_at`,
			agentID, model, database.StringArray(orig.originalTools),
			time.Now().UTC(), time.Now().UTC(),
		)
		if execErr != nil {
			return nil, fmt.Errorf("blueprint: set model for agent %q: %w", slug, execErr)
		}
	}

	return &blueprintState{
		agentID:       agentID,
		originalModel: orig.originalModel,
		originalTools: orig.originalTools,
	}, nil
}

// blueprintAssertAgentModel fetches the user-facing agent settings and checks
// that the model field matches the expected value.
func (tc *testContext) blueprintAssertAgentModel(slug, expectedModel string) error {
	agentID, err := tc.agentIDForSlug("primary", slug)
	if err != nil {
		return err
	}
	if _, err = tc.doRequest("GET", "/v1/agents/"+agentID+"/settings", "primary", nil); err != nil {
		return err
	}
	if sErr := tc.assertStatus(http.StatusOK); sErr != nil {
		return sErr
	}
	got := gjson.GetBytes(tc.lastBody, "data.model").String()
	if !strings.EqualFold(got, expectedModel) {
		return fmt.Errorf("expected agent %q model %q, got %q", slug, expectedModel, got)
	}
	return nil
}

// blueprintAddAgentTool appends a tool to the agent's allowed_tools list in
// agent_settings. If the tool is already present it is not duplicated.
// Returns a blueprintState for teardown restoration.
func (tc *testContext) blueprintAddAgentTool(alias, tool, slug string) (*blueprintState, error) {
	if err := tc.ensureDefaultWorkspace(alias); err != nil {
		return nil, err
	}
	agentID, err := tc.agentIDForSlug(alias, slug)
	if err != nil {
		return nil, err
	}

	orig := tc.blueprintSnapshot(agentID)

	// Build the new tool list, avoiding duplicates.
	newTools := make([]string, len(orig.originalTools))
	copy(newTools, orig.originalTools)
	found := false
	for _, t := range newTools {
		if t == tool {
			found = true
			break
		}
	}
	if !found {
		newTools = append(newTools, tool)
	}

	if tc.dbConn != nil {
		s := tc.sess()
		_, execErr := s.ExecContext(context.Background(), `
			INSERT INTO agent_settings (agent_id, model, allowed_tools, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (agent_id) DO UPDATE
			SET allowed_tools = EXCLUDED.allowed_tools, updated_at = EXCLUDED.updated_at`,
			agentID, orig.originalModel, database.StringArray(newTools),
			time.Now().UTC(), time.Now().UTC(),
		)
		if execErr != nil {
			return nil, fmt.Errorf("blueprint: add tool %q to agent %q: %w", tool, slug, execErr)
		}
	}

	return &blueprintState{
		agentID:       agentID,
		originalModel: orig.originalModel,
		originalTools: orig.originalTools,
	}, nil
}

// blueprintAssertAgentHasTool calls the user-facing tools endpoint and checks
// that the named tool appears in the list.
func (tc *testContext) blueprintAssertAgentHasTool(slug, expectedTool string) error {
	agentID, err := tc.agentIDForSlug("primary", slug)
	if err != nil {
		return err
	}
	if _, err = tc.doRequest("GET", "/v1/agents/"+agentID+"/tools", "primary", nil); err != nil {
		return err
	}
	if sErr := tc.assertStatus(http.StatusOK); sErr != nil {
		return sErr
	}
	for _, item := range gjson.GetBytes(tc.lastBody, "data.tools").Array() {
		if item.Get("name").String() == expectedTool {
			return nil
		}
	}
	return fmt.Errorf("expected tool %q in agent %q tool list, not found; body: %s",
		expectedTool, slug, abbreviatedBody(tc.lastBody))
}

// blueprintSnapshot reads the current agent_settings row for agentID and
// returns a partial blueprintState containing the pre-change values.
// If no row exists (settings not yet written) it returns empty defaults.
func (tc *testContext) blueprintSnapshot(agentID string) *blueprintState {
	state := &blueprintState{agentID: agentID}
	if tc.dbConn == nil {
		return state
	}
	s := tc.sess()
	var model string
	var tools database.StringArray
	row := s.QueryRowContext(context.Background(),
		`SELECT model, allowed_tools FROM agent_settings WHERE agent_id = $1`, agentID)
	if err := row.Scan(&model, &tools); err != nil {
		// No existing row is fine — defaults are empty.
		return state
	}
	state.originalModel = model
	state.originalTools = []string(tools)
	return state
}
