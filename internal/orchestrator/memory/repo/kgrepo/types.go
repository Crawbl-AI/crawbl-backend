// Package kgrepo provides the PostgreSQL implementation of the MemPalace
// knowledge graph repository. Entities live in memory_entities and
// temporal relationship triples in memory_triples; the repo owns the
// SQL, id-derivation, and dedup logic the rest of the memory subsystem
// relies on.
package kgrepo

import "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"

// Consts.

const (
	countAll         = "COUNT(*)"
	whereWorkspaceID = "workspace_id = ?"
)

const maxEntityIDPrefix = 32

// tripleResultQuery is the shared SELECT + FROM + JOIN fragment.
const tripleResultQuery = `
SELECT
    t.id, t.workspace_id, t.subject, t.predicate, t.object,
    t.valid_from, t.valid_to, t.confidence, t.source_closet, t.source_file, t.extracted_at,
    COALESCE(se.name, t.subject) AS subject_name,
    COALESCE(oe.name, t.object)  AS object_name
FROM memory_triples t
LEFT JOIN memory_entities se ON se.workspace_id = t.workspace_id AND se.id = t.subject
LEFT JOIN memory_entities oe ON oe.workspace_id = t.workspace_id AND oe.id = t.object`

// Types — structs.

// Postgres is the knowledge graph repository backed by PostgreSQL. It
// implements repo.KGRepo; callers hold it through that interface.
type Postgres struct{}

// tripleRow is used internally for scanning JOIN results.
type tripleRow struct {
	memory.Triple
	SubjectName string `db:"subject_name"`
	ObjectName  string `db:"object_name"`
}

// AddEntityOpts groups the parameters for AddEntity. ctx and sess remain
// positional per the project session/opts/repo pattern.
type AddEntityOpts struct {
	WorkspaceID string
	Name        string
	EntityType  string
	Properties  string
}

// findActiveTripleIDOpts groups the parameters for findActiveTripleID. ctx and
// sess remain positional per the project session/opts/repo pattern.
type findActiveTripleIDOpts struct {
	WorkspaceID string
	SubID       string
	Predicate   string
	ObjID       string
}
