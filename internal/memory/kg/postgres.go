package kg

import (
	"context"
	"crypto/md5"
	"fmt"
	"strings"
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

type postgresGraph struct{}

// NewPostgres creates a new knowledge Graph backed by PostgreSQL.
func NewPostgres() Graph {
	return &postgresGraph{}
}

// entityID derives a stable entity ID from a name.
func entityID(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "'", "")
	return s
}

// tripleID derives a unique triple ID from its components and a nanosecond timestamp.
func tripleID(subID, pred, objID string) string {
	raw := fmt.Sprintf("%s_%s_%s_%d", subID, pred, objID, time.Now().UnixNano())
	hash := md5.Sum([]byte(raw))
	hash8 := fmt.Sprintf("%x", hash)[:8]
	return fmt.Sprintf("t_%s_%s_%s_%s", subID, pred, objID, hash8)
}

func (g *postgresGraph) AddEntity(ctx context.Context, sess database.SessionRunner, workspaceID, name, entityType, properties string) (string, error) {
	id := entityID(name)
	now := time.Now().UTC()

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

func (g *postgresGraph) AddTriple(ctx context.Context, sess database.SessionRunner, workspaceID string, t *memory.Triple) (string, error) {
	// Check entity count limit before auto-creating entities.
	var entityCount int
	err := sess.Select("COUNT(*)").
		From("memory_entities").
		Where("workspace_id = ?", workspaceID).
		LoadOneContext(ctx, &entityCount)
	if err != nil {
		return "", fmt.Errorf("kg: count entities: %w", err)
	}
	if entityCount >= memory.MaxEntitiesPerWorkspace {
		return "", fmt.Errorf("kg: entity limit reached (%d)", memory.MaxEntitiesPerWorkspace)
	}

	// Check triple count limit.
	var tripleCount int
	err = sess.Select("COUNT(*)").
		From("memory_triples").
		Where("workspace_id = ?", workspaceID).
		LoadOneContext(ctx, &tripleCount)
	if err != nil {
		return "", fmt.Errorf("kg: count triples: %w", err)
	}
	if tripleCount >= memory.MaxTriplesPerWorkspace {
		return "", fmt.Errorf("kg: triple limit reached (%d)", memory.MaxTriplesPerWorkspace)
	}

	subID := entityID(t.Subject)
	objID := entityID(t.Object)

	// Check for existing identical active triple.
	var existingID string
	err = sess.Select("id").
		From("memory_triples").
		Where("workspace_id = ? AND subject = ? AND predicate = ? AND object = ? AND valid_to IS NULL",
			workspaceID, subID, t.Predicate, objID).
		LoadOneContext(ctx, &existingID)
	if err == nil && existingID != "" {
		return existingID, nil
	}

	// Auto-create subject and object entities (upsert — safe if they already exist).
	if _, err = g.AddEntity(ctx, sess, workspaceID, t.Subject, "entity", "{}"); err != nil {
		return "", fmt.Errorf("kg: auto-create subject entity: %w", err)
	}
	if _, err = g.AddEntity(ctx, sess, workspaceID, t.Object, "entity", "{}"); err != nil {
		return "", fmt.Errorf("kg: auto-create object entity: %w", err)
	}

	id := tripleID(subID, t.Predicate, objID)
	now := time.Now().UTC()

	_, err = sess.InsertBySql(
		`INSERT INTO memory_triples
		   (id, workspace_id, subject, predicate, object, valid_from, valid_to, confidence, source_closet, source_file, extracted_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, workspaceID, subID, t.Predicate, objID,
		t.ValidFrom, t.ValidTo, t.Confidence, t.SourceCloset, t.SourceFile, now,
	).ExecContext(ctx)
	if err != nil {
		return "", fmt.Errorf("kg: insert triple: %w", err)
	}
	return id, nil
}

func (g *postgresGraph) Invalidate(ctx context.Context, sess database.SessionRunner, workspaceID, subject, predicate, object, ended string) error {
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

func (g *postgresGraph) QueryEntity(ctx context.Context, sess database.SessionRunner, workspaceID, name, asOf, direction string) ([]memory.TripleResult, error) {
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
		args = []any{workspaceID, id}
	case "outgoing":
		whereClause = "WHERE t.workspace_id = $1 AND t.subject = $2" + asOfFilter
		args = []any{workspaceID, id}
	default: // "both"
		if asOf != "" {
			whereClause = `WHERE t.workspace_id = $1 AND (t.subject = $2 OR t.object = $2)
			               AND (t.valid_from IS NULL OR t.valid_from <= $3)
			               AND (t.valid_to IS NULL OR t.valid_to >= $3)`
		} else {
			whereClause = "WHERE t.workspace_id = $1 AND (t.subject = $2 OR t.object = $2)"
		}
		args = []any{workspaceID, id}
	}

	if asOf != "" {
		args = append(args, asOf)
	}

	query := tripleResultQuery + "\n" + whereClause + "\nORDER BY t.extracted_at DESC\nLIMIT 500"

	var rows []tripleRow
	_, err := sess.SelectBySql(query, args...).LoadContext(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("kg: query entity %q: %w", name, err)
	}

	results := make([]memory.TripleResult, len(rows))
	for i := range rows {
		results[i] = rows[i].toResult(id)
	}
	return results, nil
}

func (g *postgresGraph) QueryRelationship(ctx context.Context, sess database.SessionRunner, workspaceID, predicate, asOf string) ([]memory.TripleResult, error) {
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

	var rows []tripleRow
	_, err := sess.SelectBySql(query, args...).LoadContext(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("kg: query relationship %q: %w", predicate, err)
	}

	results := make([]memory.TripleResult, len(rows))
	for i := range rows {
		results[i] = rows[i].toResult("")
	}
	return results, nil
}

func (g *postgresGraph) Timeline(ctx context.Context, sess database.SessionRunner, workspaceID, entityName string) ([]memory.TripleResult, error) {
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

	var rows []tripleRow
	_, err := sess.SelectBySql(query, args...).LoadContext(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("kg: timeline: %w", err)
	}

	results := make([]memory.TripleResult, len(rows))
	for i := range rows {
		results[i] = rows[i].toResult("")
	}
	return results, nil
}

func (g *postgresGraph) Stats(ctx context.Context, sess database.SessionRunner, workspaceID string) (*memory.KGStats, error) {
	var entities int
	err := sess.Select("COUNT(*)").
		From("memory_entities").
		Where("workspace_id = ?", workspaceID).
		LoadOneContext(ctx, &entities)
	if err != nil {
		return nil, fmt.Errorf("kg: stats entities: %w", err)
	}

	var triples int
	err = sess.Select("COUNT(*)").
		From("memory_triples").
		Where("workspace_id = ?", workspaceID).
		LoadOneContext(ctx, &triples)
	if err != nil {
		return nil, fmt.Errorf("kg: stats triples: %w", err)
	}

	var current int
	err = sess.Select("COUNT(*)").
		From("memory_triples").
		Where("workspace_id = ? AND valid_to IS NULL", workspaceID).
		LoadOneContext(ctx, &current)
	if err != nil {
		return nil, fmt.Errorf("kg: stats current: %w", err)
	}

	var predicates []string
	_, err = sess.Select("DISTINCT predicate").
		From("memory_triples").
		Where("workspace_id = ?", workspaceID).
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
