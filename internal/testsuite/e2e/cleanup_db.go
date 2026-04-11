// Package e2e — hard DB cleanup for the e2e suite.
//
// The API /v1/auth/delete endpoint does a *soft* delete — it sets
// users.deleted_at but leaves the row (and every related workspace,
// agent, conversation, memory, usage counter…) in place. That was
// fine for a handful of runs, but over hundreds of iterations the
// orchestrator DB accumulates thousands of zombie rows and the test
// scenarios start drifting against noise they didn't create.
//
// wipeE2EResidue performs a hard-delete purge of every row that
// belongs to a user whose subject matches the e2e suffix pattern
// (`e2e-%`). It is safe to call at suite entry and suite exit:
//
//   - 17 of the 21 relevant tables have ON DELETE CASCADE to
//     users/workspaces, so a single `DELETE FROM users` handles
//     them transitively.
//
//   - 4 tables (memory_drawers, memory_entities, memory_identities,
//     memory_triples) reference workspace_id as a plain TEXT column
//     with no FK, so they must be wiped explicitly before the user
//     delete cascades the workspace out from under them.
//
//   - mcp_audit_logs uses SET NULL on cascade, which would leave
//     orphaned audit rows with a null workspace_id. Those are
//     harmless for the product but polluting for repeated e2e runs,
//     so we drop them explicitly too.
//
// This helper is a no-op when the suite runs without a DB handle
// (the CI --base-url-only path).
package e2e

import (
	"context"
	"log"

	"github.com/gocraft/dbr/v2"
)

// e2eSubjectPattern is the LIKE pattern every test user's subject
// matches. It is set once in Run() from the suite's fixed
// `e2e-<alias>-<unix-ns>` subject format.
const e2eSubjectPattern = "e2e-%"

// memoryWorkspaceTables is the set of memory_* tables that store a
// workspace_id without an ON DELETE CASCADE foreign key, so they
// must be wiped explicitly before the user delete runs.
var memoryWorkspaceTables = []string{
	"memory_drawers",
	"memory_entities",
	"memory_identities",
	"memory_triples",
}

// wipeE2EResidue hard-deletes every row belonging to any current or
// soft-deleted e2e test user. Safe to call multiple times per run.
func wipeE2EResidue(deps *suiteDeps) {
	if deps == nil || deps.db == nil {
		return
	}
	sess := deps.db.NewSession(nil)
	ctx := context.Background()

	// Step 1 — collect workspace IDs once so every subsequent delete
	// uses the same snapshot. Avoids a race where a late-arriving
	// e2e signup creates rows under a workspace we already purged.
	var workspaceIDs []string
	if _, err := sess.Select("w.id").
		From(dbr.I("workspaces").As("w")).
		Join(dbr.I("users").As("u"), "u.id = w.user_id").
		Where("u.subject LIKE ?", e2eSubjectPattern).
		LoadContext(ctx, &workspaceIDs); err != nil {
		log.Printf("e2e cleanup: collect workspace ids: %v", err)
		return
	}

	// Step 2 — drop every memory_* row bound to those workspaces.
	// These tables have no FK cascade, so cascading on user delete
	// would leave them dangling.
	if len(workspaceIDs) > 0 {
		for _, table := range memoryWorkspaceTables {
			if _, err := sess.DeleteFrom(table).
				Where("workspace_id IN ?", workspaceIDs).
				ExecContext(ctx); err != nil {
				log.Printf("e2e cleanup: wipe %s: %v", table, err)
			}
		}

		// mcp_audit_logs uses SET NULL on workspace cascade, which
		// keeps orphaned audit rows around. Drop them explicitly so
		// repeated runs do not pile up audit residue.
		if _, err := sess.DeleteFrom("mcp_audit_logs").
			Where("workspace_id IN ?", workspaceIDs).
			ExecContext(ctx); err != nil {
			log.Printf("e2e cleanup: wipe mcp_audit_logs: %v", err)
		}
	}

	// Step 3 — hard-delete the users. ON DELETE CASCADE propagates
	// through workspaces → agents → conversations → messages →
	// agent_settings / agent_prompts / agent_usage_counters /
	// memory_triggers / agent_triggers / user_preferences /
	// user_push_tokens / usage_counters / usage_quotas /
	// integration_connections / workflow_definitions /
	// workflow_executions / artifacts / agent_messages /
	// agent_delegations.
	res, err := sess.DeleteFrom("users").
		Where("subject LIKE ?", e2eSubjectPattern).
		ExecContext(ctx)
	if err != nil {
		log.Printf("e2e cleanup: delete users: %v", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("e2e cleanup: purged %d e2e user(s) + %d workspace(s) and all cascaded rows", n, len(workspaceIDs))
	}
}
