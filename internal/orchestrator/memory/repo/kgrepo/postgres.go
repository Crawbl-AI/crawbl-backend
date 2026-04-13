// Package kgrepo provides the PostgreSQL implementation of the MemPalace
// knowledge graph repository. Entities live in memory_entities and
// temporal relationship triples in memory_triples; the repo owns the
// SQL, id-derivation, and dedup logic the rest of the memory subsystem
// relies on.
package kgrepo

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/gocraft/dbr/v2"

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

// Postgres is the knowledge graph repository backed by PostgreSQL. It
// implements repo.KGRepo; callers hold it through that interface.
type Postgres struct{}

// NewPostgres creates a new knowledge graph repository backed by PostgreSQL.
func NewPostgres() *Postgres {
	return &Postgres{}
}

// entityID derives a stable, collision-resistant entity ID from a name.
func entityID(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return "entity_empty"
	}
	h := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("e_%s_%x", sanitizeForID(normalized), h[:4])
}

const maxEntityIDPrefix = 32

const (
	selectCount    = "COUNT(*)"
	whereWorkspace = "workspace_id = ?"
)

// sanitizeForID keeps the name readable but safe for use in IDs.
func sanitizeForID(s string) string {
	s = strings.ReplaceAll(s, " ", "_")
	if len(s) > maxEntityIDPrefix {
		s = s[:32]
	}
	return s
}

// tripleID derives a unique triple ID from its components and a nanosecond timestamp.
func tripleID(subID, pred, objID string) string {
	raw := fmt.Sprintf("%s_%s_%s_%d", subID, pred, objID, time.Now().UnixNano())
	hash := md5.Sum([]byte(raw))
	return fmt.Sprintf("t_%s_%s_%s_%x", subID, pred, objID, hash[:4])
}

func (g *Postgres) AddEntity(ctx context.Context, sess database.SessionRunner, workspaceID, name, entityType, properties string) (string, error) {
	id := entityID(name)
	now := time.Now().UTC()

	// ON CONFLICT DO UPDATE (upsert) is not supported by the dbr builder; raw SQL required.
	_, err := sess.InsertBySql(
		`INSERT INTO memory_entities (id, workspace_id, name, type, properties, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (workspace_id, id) DO UPDATE
		   SET name = EXCLUDED.name,
		       type = EXCLUDED.type,
		       properties = EXCLUDED.properties`,
		id, workspaceID, name, entityType, properties, now,
	).ExecContext(ctx)
	if err != nil {
		return "", fmt.Errorf("kg: add entity %q: %w", name, err)
	}
	return id, nil
}

func (g *Postgres) AddTriple(ctx context.Context, sess database.SessionRunner, workspaceID string, t *memory.Triple) (string, error) {
	if err := g.checkWorkspaceLimits(ctx, sess, workspaceID); err != nil {
		return "", err
	}

	subID := entityID(t.Subject)
	objID := entityID(t.Object)

	if existing, err := g.findActiveTripleID(ctx, sess, workspaceID, subID, t.Predicate, objID); err == nil && existing != "" {
		return existing, nil
	}

	if err := g.ensureTripleEntities(ctx, sess, workspaceID, t); err != nil {
		return "", err
	}

	id := tripleID(subID, t.Predicate, objID)
	now := time.Now().UTC()
	_, err := sess.InsertInto("memory_triples").
		Columns("id", "workspace_id", "subject", "predicate", "object",
			"valid_from", "valid_to", "confidence", "source_closet", "source_file", "extracted_at").
		Values(id, workspaceID, subID, t.Predicate, objID,
			t.ValidFrom, t.ValidTo, t.Confidence, t.SourceCloset, t.SourceFile, now).
		ExecContext(ctx)
	if err != nil {
		return "", fmt.Errorf("kg: insert triple: %w", err)
	}
	return id, nil
}

// checkWorkspaceLimits enforces the per-workspace entity and triple caps
// before we auto-create endpoints for a new triple. Returns an error when
// either quota is exceeded so AddTriple can bail without inserting.
func (g *Postgres) checkWorkspaceLimits(ctx context.Context, sess database.SessionRunner, workspaceID string) error {
	var entityCount int
	if err := sess.Select(selectCount).
		From("memory_entities").
		Where(whereWorkspace, workspaceID).
		LoadOneContext(ctx, &entityCount); err != nil {
		return fmt.Errorf("kg: count entities: %w", err)
	}
	if entityCount >= memory.MaxEntitiesPerWorkspace {
		return fmt.Errorf("kg: entity limit reached (%d)", memory.MaxEntitiesPerWorkspace)
	}

	var tripleCount int
	if err := sess.Select(selectCount).
		From("memory_triples").
		Where(whereWorkspace, workspaceID).
		LoadOneContext(ctx, &tripleCount); err != nil {
		return fmt.Errorf("kg: count triples: %w", err)
	}
	if tripleCount >= memory.MaxTriplesPerWorkspace {
		return fmt.Errorf("kg: triple limit reached (%d)", memory.MaxTriplesPerWorkspace)
	}
	return nil
}

// findActiveTripleID returns the id of an existing active triple with the
// same subject/predicate/object tuple, or an empty string if no match.
// Callers use this to dedup concurrent inserts.
func (g *Postgres) findActiveTripleID(ctx context.Context, sess database.SessionRunner, workspaceID, subID, predicate, objID string) (string, error) {
	var existingID string
	err := sess.Select("id").
		From("memory_triples").
		Where("workspace_id = ? AND subject = ? AND predicate = ? AND object = ? AND valid_to IS NULL",
			workspaceID, subID, predicate, objID).
		LoadOneContext(ctx, &existingID)
	return existingID, err
}

// ensureTripleEntities upserts the subject and object entity rows for a
// triple. Both are safe to re-run — AddEntity uses ON CONFLICT — but
// either error aborts the caller so we never insert a dangling triple.
func (g *Postgres) ensureTripleEntities(ctx context.Context, sess database.SessionRunner, workspaceID string, t *memory.Triple) error {
	if _, err := g.AddEntity(ctx, sess, workspaceID, t.Subject, "entity", "{}"); err != nil {
		return fmt.Errorf("kg: auto-create subject entity: %w", err)
	}
	if _, err := g.AddEntity(ctx, sess, workspaceID, t.Object, "entity", "{}"); err != nil {
		return fmt.Errorf("kg: auto-create object entity: %w", err)
	}
	return nil
}

func (g *Postgres) Invalidate(ctx context.Context, sess database.SessionRunner, workspaceID, subject, predicate, object, ended string) error {
	subID := entityID(subject)
	objID := entityID(object)

	_, err := sess.Update("memory_triples").
		Set("valid_to", ended).
		Where("workspace_id = ? AND subject = ? AND predicate = ? AND object = ? AND valid_to IS NULL",
			workspaceID, subID, predicate, objID).
		ExecContext(ctx)
	if err != nil {
		return fmt.Errorf("kg: invalidate triple: %w", err)
	}
	return nil
}

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

func (g *Postgres) QueryEntity(ctx context.Context, sess database.SessionRunner, workspaceID, name, asOf, direction string) ([]memory.TripleResult, error) {
	id := entityID(name)

	var whereClause string
	var args []any

	asOfFilter := ""
	if asOf != "" {
		asOfFilter = " AND (t.valid_from IS NULL OR t.valid_from <= $3) AND (t.valid_to IS NULL OR t.valid_to >= $3)"
	}

	switch direction {
	case "incoming":
		whereClause = "WHERE t.workspace_id = $1 AND t.object = $2" + asOfFilter
	case "outgoing":
		whereClause = "WHERE t.workspace_id = $1 AND t.subject = $2" + asOfFilter
	default: // "both"
		whereClause = "WHERE t.workspace_id = $1 AND (t.subject = $2 OR t.object = $2)" + asOfFilter
	}
	args = []any{workspaceID, id}
	if asOf != "" {
		args = append(args, asOf)
	}

	query := tripleResultQuery + "\n" + whereClause + "\nORDER BY t.extracted_at DESC\nLIMIT 500"

	// NOTE: the "both" direction reuses $2 twice (subject = $2 OR object = $2),
	// and when asOf is set $3 appears twice as well. dbr's SelectBySql counts
	// $N occurrences instead of unique numbers and raises "wrong placeholder
	// count". Use raw db.QueryContext instead.
	db, ok := sess.(*dbr.Session)
	if !ok || db == nil || db.DB == nil {
		return nil, fmt.Errorf("kg: query entity %q: session is not a *dbr.Session with a live connection", name)
	}
	sqlRows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("kg: query entity %q: %w", name, err)
	}
	defer func() { _ = sqlRows.Close() }()

	var rows []tripleRow
	for sqlRows.Next() {
		var row tripleRow
		if scanErr := sqlRows.Scan(
			&row.ID, &row.WorkspaceID, &row.Subject, &row.Predicate, &row.Object,
			&row.ValidFrom, &row.ValidTo, &row.Confidence, &row.SourceCloset, &row.SourceFile, &row.ExtractedAt,
			&row.SubjectName, &row.ObjectName,
		); scanErr != nil {
			return nil, fmt.Errorf("kg: query entity %q scan: %w", name, scanErr)
		}
		rows = append(rows, row)
	}
	if rowsErr := sqlRows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("kg: query entity %q iterate: %w", name, rowsErr)
	}

	results := make([]memory.TripleResult, len(rows))
	for i := range rows {
		results[i] = rows[i].toResult(id)
	}
	return results, nil
}

func (g *Postgres) QueryRelationship(ctx context.Context, sess database.SessionRunner, workspaceID, predicate, asOf string) ([]memory.TripleResult, error) {
	var query string
	var args []any

	if asOf != "" {
		query = tripleResultQuery + `
WHERE t.workspace_id = $1 AND t.predicate = $2
  AND (t.valid_from IS NULL OR t.valid_from <= $3)
  AND (t.valid_to IS NULL OR t.valid_to >= $3)
ORDER BY t.extracted_at DESC
LIMIT 500`
		args = []any{workspaceID, predicate, asOf}
	} else {
		query = tripleResultQuery + `
WHERE t.workspace_id = $1 AND t.predicate = $2
ORDER BY t.extracted_at DESC
LIMIT 500`
		args = []any{workspaceID, predicate}
	}

	// NOTE: when asOf is set, $3 appears twice (valid_from <= $3 AND valid_to >= $3);
	// dbr's SelectBySql raises "wrong placeholder count". Use raw db.QueryContext.
	db, ok := sess.(*dbr.Session)
	if !ok || db == nil || db.DB == nil {
		return nil, fmt.Errorf("kg: query relationship %q: session is not a *dbr.Session with a live connection", predicate)
	}
	sqlRows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("kg: query relationship %q: %w", predicate, err)
	}
	defer func() { _ = sqlRows.Close() }()

	var rows []tripleRow
	for sqlRows.Next() {
		var row tripleRow
		if scanErr := sqlRows.Scan(
			&row.ID, &row.WorkspaceID, &row.Subject, &row.Predicate, &row.Object,
			&row.ValidFrom, &row.ValidTo, &row.Confidence, &row.SourceCloset, &row.SourceFile, &row.ExtractedAt,
			&row.SubjectName, &row.ObjectName,
		); scanErr != nil {
			return nil, fmt.Errorf("kg: query relationship %q scan: %w", predicate, scanErr)
		}
		rows = append(rows, row)
	}
	if rowsErr := sqlRows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("kg: query relationship %q iterate: %w", predicate, rowsErr)
	}

	results := make([]memory.TripleResult, len(rows))
	for i := range rows {
		results[i] = rows[i].toResult("")
	}
	return results, nil
}

func (g *Postgres) Timeline(ctx context.Context, sess database.SessionRunner, workspaceID, entityName string) ([]memory.TripleResult, error) {
	var query string
	var args []any

	if entityName != "" {
		id := entityID(entityName)
		query = tripleResultQuery + `
WHERE t.workspace_id = $1 AND (t.subject = $2 OR t.object = $2)
ORDER BY t.valid_from ASC NULLS LAST
LIMIT 100`
		args = []any{workspaceID, id}
	} else {
		query = tripleResultQuery + `
WHERE t.workspace_id = $1
ORDER BY t.valid_from ASC NULLS LAST
LIMIT 100`
		args = []any{workspaceID}
	}

	// NOTE: when entityName is set, $2 appears twice (subject = $2 OR object = $2);
	// dbr's SelectBySql raises "wrong placeholder count". Use raw db.QueryContext.
	db, ok := sess.(*dbr.Session)
	if !ok || db == nil || db.DB == nil {
		return nil, fmt.Errorf("kg: timeline: session is not a *dbr.Session with a live connection")
	}
	sqlRows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("kg: timeline: %w", err)
	}
	defer func() { _ = sqlRows.Close() }()

	var rows []tripleRow
	for sqlRows.Next() {
		var row tripleRow
		if scanErr := sqlRows.Scan(
			&row.ID, &row.WorkspaceID, &row.Subject, &row.Predicate, &row.Object,
			&row.ValidFrom, &row.ValidTo, &row.Confidence, &row.SourceCloset, &row.SourceFile, &row.ExtractedAt,
			&row.SubjectName, &row.ObjectName,
		); scanErr != nil {
			return nil, fmt.Errorf("kg: timeline scan: %w", scanErr)
		}
		rows = append(rows, row)
	}
	if rowsErr := sqlRows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("kg: timeline iterate: %w", rowsErr)
	}

	results := make([]memory.TripleResult, len(rows))
	for i := range rows {
		results[i] = rows[i].toResult("")
	}
	return results, nil
}

func (g *Postgres) Stats(ctx context.Context, sess database.SessionRunner, workspaceID string) (*memory.KGStats, error) {
	var entities int
	err := sess.Select(selectCount).
		From("memory_entities").
		Where(whereWorkspace, workspaceID).
		LoadOneContext(ctx, &entities)
	if err != nil {
		return nil, fmt.Errorf("kg: stats entities: %w", err)
	}

	var triples int
	err = sess.Select(selectCount).
		From("memory_triples").
		Where(whereWorkspace, workspaceID).
		LoadOneContext(ctx, &triples)
	if err != nil {
		return nil, fmt.Errorf("kg: stats triples: %w", err)
	}

	var current int
	err = sess.Select(selectCount).
		From("memory_triples").
		Where("workspace_id = ? AND valid_to IS NULL", workspaceID).
		LoadOneContext(ctx, &current)
	if err != nil {
		return nil, fmt.Errorf("kg: stats current: %w", err)
	}

	var predicates []string
	_, err = sess.Select("DISTINCT predicate").
		From("memory_triples").
		Where(whereWorkspace, workspaceID).
		OrderDir("predicate", true).
		LoadContext(ctx, &predicates)
	if err != nil {
		return nil, fmt.Errorf("kg: stats predicates: %w", err)
	}

	return &memory.KGStats{
		Entities:          entities,
		Triples:           triples,
		CurrentFacts:      current,
		ExpiredFacts:      triples - current,
		RelationshipTypes: predicates,
	}, nil
}

// tripleRow is used internally for scanning JOIN results.
type tripleRow struct {
	memory.Triple
	SubjectName string `db:"subject_name"`
	ObjectName  string `db:"object_name"`
}

func (r *tripleRow) toResult(queryEntityID string) memory.TripleResult {
	res := memory.TripleResult{
		Triple:      r.Triple,
		SubjectName: r.SubjectName,
		ObjectName:  r.ObjectName,
		Current:     r.ValidTo == nil,
	}
	if queryEntityID != "" {
		if r.Subject == queryEntityID {
			res.Direction = "outgoing"
		} else {
			res.Direction = "incoming"
		}
	}
	return res
}
