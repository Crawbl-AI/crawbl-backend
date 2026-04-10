// Package drawerrepo provides the PostgreSQL implementation of the
// MemPalace drawer repository. Drawers are the vector-searchable chunks
// stored in memory_drawers, with embeddings managed through pgvector and
// hybrid semantic/knowledge-graph retrieval exposed via SearchHybrid.
package drawerrepo

import (
	"context"
	"fmt"
	"strings"

	"github.com/gocraft/dbr/v2"
	"github.com/lib/pq"
	"github.com/pgvector/pgvector-go"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

// Postgres is the memory_drawers repository backed by PostgreSQL with
// pgvector for embeddings. It implements repo.DrawerRepo; callers hold it
// through that interface.
type Postgres struct{}

// NewPostgres creates a new drawer repository backed by PostgreSQL with pgvector.
func NewPostgres() *Postgres {
	return &Postgres{}
}

func (r *Postgres) Add(ctx context.Context, sess database.SessionRunner, d *memory.Drawer, embedding []float32) error {
	count, err := r.Count(ctx, sess, d.WorkspaceID)
	if err != nil {
		return fmt.Errorf("drawer: check count: %w", err)
	}
	if count >= memory.MaxDrawersPerWorkspace {
		return fmt.Errorf("drawer: workspace limit reached (%d)", memory.MaxDrawersPerWorkspace)
	}

	if len(embedding) > 0 {
		vec := pgvector.NewVector(embedding)
		_, err := sess.InsertBySql(
			`INSERT INTO memory_drawers (id, workspace_id, wing, room, hall, content, embedding, importance, memory_type, source_file, added_by, added_by_agent, state, filed_at, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			d.ID, d.WorkspaceID, d.Wing, d.Room, d.Hall, d.Content, vec,
			d.Importance, d.MemoryType, d.SourceFile, d.AddedBy, d.AddedByAgent, d.State, d.FiledAt, d.CreatedAt,
		).ExecContext(ctx)
		return err
	}

	_, err = sess.InsertBySql(
		`INSERT INTO memory_drawers (id, workspace_id, wing, room, hall, content, importance, memory_type, source_file, added_by, added_by_agent, state, filed_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.WorkspaceID, d.Wing, d.Room, d.Hall, d.Content,
		d.Importance, d.MemoryType, d.SourceFile, d.AddedBy, d.AddedByAgent, d.State, d.FiledAt, d.CreatedAt,
	).ExecContext(ctx)
	return err
}

func (r *Postgres) Delete(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string) error {
	_, err := sess.DeleteFrom("memory_drawers").
		Where("workspace_id = ? AND id = ?", workspaceID, drawerID).
		ExecContext(ctx)
	return err
}

func (r *Postgres) Search(ctx context.Context, sess database.SessionRunner, workspaceID string, queryEmbedding []float32, wing, room string, limit int) ([]memory.DrawerSearchResult, error) {
	if len(queryEmbedding) == 0 {
		return nil, fmt.Errorf("drawer: empty query embedding")
	}
	if limit <= 0 {
		limit = 5
	}

	vec := pgvector.NewVector(queryEmbedding)

	where := "workspace_id = $2 AND embedding IS NOT NULL AND (state IN ('raw', 'processed') OR state = '') AND superseded_by IS NULL"
	args := []any{vec, workspaceID}
	paramIdx := 3

	if wing != "" {
		where += fmt.Sprintf(" AND wing = $%d", paramIdx)
		args = append(args, wing)
		paramIdx++
	}
	if room != "" {
		where += fmt.Sprintf(" AND room = $%d", paramIdx)
		args = append(args, room)
		paramIdx++
	}

	args = append(args, limit)
	query := fmt.Sprintf(
		`SELECT id, workspace_id, wing, room, hall, content, importance, memory_type,
		        source_file, added_by, filed_at, created_at,
		        state, summary, added_by_agent,
		        1 - (embedding <=> $1) AS similarity
		 FROM memory_drawers
		 WHERE %s
		 ORDER BY embedding <=> $1
		 LIMIT $%d`,
		where, paramIdx,
	)

	var results []memory.DrawerSearchResult
	_, err := sess.SelectBySql(query, args...).LoadContext(ctx, &results)
	if err != nil {
		return nil, fmt.Errorf("drawer: search: %w", err)
	}
	return results, nil
}

// hybridSearchSQL is the single-round-trip CTE used by SearchHybrid.
//
// $1 = query embedding (pgvector)
// $2 = workspace_id
// $3 = vector candidate limit (typically limit*2)
// $4 = lowercased query terms as text[]
// $5 = final limit
//
// vector_hits pulls the top-N ANN matches from memory_drawers. kg_hits finds
// drawer IDs whose triples touch an entity whose lowercased name is in $4.
// The merged CTE unions both, keeps the max similarity per drawer, and the
// outer query re-joins memory_drawers to return the full row.
const hybridSearchSQL = `
WITH vector_hits AS (
    SELECT id, 1 - (embedding <=> $1) AS similarity, false AS via_kg
    FROM memory_drawers
    WHERE workspace_id = $2
      AND embedding IS NOT NULL
      AND (state IN ('raw', 'processed') OR state = '')
      AND superseded_by IS NULL
    ORDER BY embedding <=> $1
    LIMIT $3
),
kg_hits AS (
    SELECT DISTINCT t.source_closet AS id, 0.0::float8 AS similarity, true AS via_kg
    FROM memory_triples t
    JOIN memory_entities e
      ON e.workspace_id = t.workspace_id
     AND (e.id = t.subject OR e.id = t.object)
    WHERE t.workspace_id = $2
      AND t.source_closet <> ''
      AND lower(e.name) = ANY($4::text[])
),
merged AS (
    SELECT id,
           MAX(similarity)::float8 AS similarity,
           bool_or(via_kg)         AS via_kg
    FROM (
        SELECT * FROM vector_hits
        UNION ALL
        SELECT * FROM kg_hits
    ) u
    GROUP BY id
)
SELECT d.id, d.workspace_id, d.wing, d.room, d.hall, d.content,
       d.importance, d.memory_type, d.source_file, d.added_by, d.added_by_agent,
       d.filed_at, d.created_at, d.state, d.summary, d.last_accessed_at,
       d.access_count, d.superseded_by, d.cluster_id, d.retry_count,
       m.similarity, m.via_kg
FROM merged m
JOIN memory_drawers d
  ON d.id = m.id AND d.workspace_id = $2
WHERE d.superseded_by IS NULL
  AND d.state <> 'merged'
ORDER BY m.similarity DESC
LIMIT $5
`

func (r *Postgres) SearchHybrid(
	ctx context.Context,
	sess database.SessionRunner,
	workspaceID string,
	queryEmbedding []float32,
	queryTerms []string,
	limit int,
) ([]memory.HybridSearchResult, error) {
	if len(queryEmbedding) == 0 {
		return nil, fmt.Errorf("drawerrepo: empty query embedding")
	}
	if limit <= 0 {
		limit = 5
	}

	// Lowercase defensively; callers are expected to do this already but the
	// SQL = ANY comparison is case-sensitive so a safety pass here is cheap.
	terms := make([]string, 0, len(queryTerms))
	for _, t := range queryTerms {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" {
			terms = append(terms, t)
		}
	}

	vec := pgvector.NewVector(queryEmbedding)
	vectorLimit := limit * 2

	var results []memory.HybridSearchResult
	_, err := sess.SelectBySql(
		hybridSearchSQL,
		vec, workspaceID, vectorLimit, pq.Array(terms), limit,
	).LoadContext(ctx, &results)
	if err != nil {
		return nil, fmt.Errorf("drawerrepo: hybrid search: %w", err)
	}
	return results, nil
}

func (r *Postgres) CheckDuplicate(ctx context.Context, sess database.SessionRunner, workspaceID string, embedding []float32, threshold float64, limit int) ([]memory.DrawerSearchResult, error) {
	if len(embedding) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}

	vec := pgvector.NewVector(embedding)
	query := `SELECT id, workspace_id, wing, room, hall, content, importance, memory_type,
	                 source_file, added_by, filed_at, created_at,
	                 state, summary, added_by_agent,
	                 1 - (embedding <=> $1) AS similarity
	          FROM memory_drawers
	          WHERE workspace_id = $2 AND embedding IS NOT NULL
	            AND 1 - (embedding <=> $1) >= $3
	          ORDER BY embedding <=> $1
	          LIMIT $4`

	var results []memory.DrawerSearchResult
	_, err := sess.SelectBySql(query, vec, workspaceID, threshold, limit).LoadContext(ctx, &results)
	if err != nil {
		return nil, fmt.Errorf("drawer: check duplicate: %w", err)
	}
	return results, nil
}

func (r *Postgres) Count(ctx context.Context, sess database.SessionRunner, workspaceID string) (int, error) {
	var count int
	err := sess.Select("COUNT(*)").
		From("memory_drawers").
		Where("workspace_id = ?", workspaceID).
		LoadOneContext(ctx, &count)
	if err != nil {
		return 0, fmt.Errorf("drawer: count: %w", err)
	}
	return count, nil
}

func (r *Postgres) ListWings(ctx context.Context, sess database.SessionRunner, workspaceID string) ([]memory.WingCount, error) {
	var results []memory.WingCount
	_, err := sess.Select("wing", "COUNT(*) AS count").
		From("memory_drawers").
		Where("workspace_id = ?", workspaceID).
		GroupBy("wing").
		OrderDir("count", false).
		LoadContext(ctx, &results)
	if err != nil {
		return nil, fmt.Errorf("drawer: list wings: %w", err)
	}
	return results, nil
}

func (r *Postgres) ListRooms(ctx context.Context, sess database.SessionRunner, workspaceID, wing string) ([]memory.RoomCount, error) {
	q := sess.Select("wing", "room", "COUNT(*) AS count").
		From("memory_drawers").
		Where("workspace_id = ?", workspaceID)
	if wing != "" {
		q = q.Where("wing = ?", wing)
	}
	var results []memory.RoomCount
	_, err := q.GroupBy("wing", "room").
		OrderDir("count", false).
		LoadContext(ctx, &results)
	if err != nil {
		return nil, fmt.Errorf("drawer: list rooms: %w", err)
	}
	return results, nil
}

func (r *Postgres) GetTopByImportance(ctx context.Context, sess database.SessionRunner, workspaceID, wing string, limit int) ([]memory.Drawer, error) {
	if limit <= 0 {
		limit = 15
	}
	q := sess.Select("id", "workspace_id", "wing", "room", "hall", "content",
		"importance", "memory_type", "source_file", "added_by", "filed_at", "created_at").
		From("memory_drawers").
		Where("workspace_id = ?", workspaceID)
	if wing != "" {
		q = q.Where("wing = ?", wing)
	}
	var results []memory.Drawer
	_, err := q.Where("superseded_by IS NULL").Where("state != 'merged'").OrderDir("importance", false).
		Limit(uint64(limit)).
		LoadContext(ctx, &results)
	if err != nil {
		return nil, fmt.Errorf("drawer: top by importance: %w", err)
	}
	return results, nil
}

func (r *Postgres) ListByWorkspace(ctx context.Context, sess database.SessionRunner, workspaceID string, limit, offset int) ([]memory.Drawer, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var rows []memory.Drawer
	_, err := sess.Select("id", "workspace_id", "wing", "room", "hall", "content", "importance", "memory_type", "source_file", "added_by", "filed_at", "created_at").
		From("memory_drawers").
		Where("workspace_id = ?", workspaceID).
		OrderBy("filed_at DESC").
		Limit(uint64(limit)).
		Offset(uint64(offset)).
		LoadContext(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("drawer: list by workspace: %w", err)
	}
	return rows, nil
}

func (r *Postgres) GetByWingRoom(ctx context.Context, sess database.SessionRunner, workspaceID, wing, room string, limit int) ([]memory.Drawer, error) {
	if limit <= 0 {
		limit = 10
	}
	q := sess.Select("id", "workspace_id", "wing", "room", "hall", "content",
		"importance", "memory_type", "source_file", "added_by", "filed_at", "created_at").
		From("memory_drawers").
		Where("workspace_id = ?", workspaceID)
	if wing != "" {
		q = q.Where("wing = ?", wing)
	}
	if room != "" {
		q = q.Where("room = ?", room)
	}
	var results []memory.Drawer
	_, err := q.Where("superseded_by IS NULL").Where("state != 'merged'").Limit(uint64(limit)).
		LoadContext(ctx, &results)
	if err != nil {
		return nil, fmt.Errorf("drawer: get by wing/room: %w", err)
	}
	return results, nil
}

func (r *Postgres) AddIdempotent(ctx context.Context, sess database.SessionRunner, d *memory.Drawer, embedding []float32) error {
	if len(embedding) > 0 {
		vec := pgvector.NewVector(embedding)
		_, err := sess.InsertBySql(
			`INSERT INTO memory_drawers (id, workspace_id, wing, room, hall, content, embedding, importance, memory_type, source_file, added_by, added_by_agent, state, filed_at, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT DO NOTHING`,
			d.ID, d.WorkspaceID, d.Wing, d.Room, d.Hall, d.Content, vec,
			d.Importance, d.MemoryType, d.SourceFile, d.AddedBy, d.AddedByAgent, d.State, d.FiledAt, d.CreatedAt,
		).ExecContext(ctx)
		return err
	}

	_, err := sess.InsertBySql(
		`INSERT INTO memory_drawers (id, workspace_id, wing, room, hall, content, importance, memory_type, source_file, added_by, added_by_agent, state, filed_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT DO NOTHING`,
		d.ID, d.WorkspaceID, d.Wing, d.Room, d.Hall, d.Content,
		d.Importance, d.MemoryType, d.SourceFile, d.AddedBy, d.AddedByAgent, d.State, d.FiledAt, d.CreatedAt,
	).ExecContext(ctx)
	return err
}

func (r *Postgres) ListByState(ctx context.Context, sess database.SessionRunner, workspaceID, state string, limit int) ([]memory.Drawer, error) {
	if limit <= 0 {
		limit = 10
	}
	var rows []memory.Drawer
	_, err := sess.SelectBySql(
		`SELECT id, workspace_id, wing, room, hall, content, importance, memory_type,
		        source_file, added_by, filed_at, created_at, state, retry_count
		 FROM memory_drawers
		 WHERE workspace_id = ? AND state = ?
		 ORDER BY created_at ASC
		 LIMIT ?
		 FOR UPDATE SKIP LOCKED`,
		workspaceID, state, limit,
	).LoadContext(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("drawer: list by state: %w", err)
	}
	return rows, nil
}

func (r *Postgres) UpdateState(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID, state string) error {
	_, err := sess.Update("memory_drawers").
		Set("state", state).
		Where("workspace_id = ? AND id = ?", workspaceID, drawerID).
		ExecContext(ctx)
	return err
}

func (r *Postgres) UpdateClassification(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID, memoryType, summary, room string, importance float64) error {
	_, err := sess.Update("memory_drawers").
		Set("memory_type", memoryType).
		Set("summary", summary).
		Set("room", room).
		Set("importance", importance).
		Where("workspace_id = ? AND id = ?", workspaceID, drawerID).
		ExecContext(ctx)
	return err
}

func (r *Postgres) SetSupersededBy(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID, supersededBy string) error {
	_, err := sess.Update("memory_drawers").
		Set("superseded_by", supersededBy).
		Where("workspace_id = ? AND id = ?", workspaceID, drawerID).
		ExecContext(ctx)
	return err
}

func (r *Postgres) SetClusterID(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID, clusterID string) error {
	_, err := sess.Update("memory_drawers").
		Set("cluster_id", clusterID).
		Where("workspace_id = ? AND id = ?", workspaceID, drawerID).
		ExecContext(ctx)
	return err
}

func (r *Postgres) TouchAccess(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string) error {
	_, err := sess.Update("memory_drawers").
		Set("last_accessed_at", dbr.Expr("NOW()")).
		Set("access_count", dbr.Expr("access_count + 1")).
		Where("workspace_id = ? AND id = ?", workspaceID, drawerID).
		ExecContext(ctx)
	return err
}

func (r *Postgres) IncrementRetryCount(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string) error {
	_, err := sess.Update("memory_drawers").
		Set("retry_count", dbr.Expr("retry_count + 1")).
		Where("workspace_id = ? AND id = ?", workspaceID, drawerID).
		ExecContext(ctx)
	return err
}

func (r *Postgres) DecayImportance(ctx context.Context, sess database.SessionRunner, workspaceID string, olderThanDays, skipAccessedWithinDays int, factor, floor float64) (int, error) {
	res, err := sess.Update("memory_drawers").
		Set("importance", dbr.Expr("GREATEST(importance * ?, ?)", factor, floor)).
		Where("workspace_id = ?", workspaceID).
		Where("state = ?", "processed").
		Where("importance > ?", floor).
		Where(dbr.Expr("created_at < NOW() - INTERVAL '1 day' * ?", olderThanDays)).
		Where(dbr.Expr("(last_accessed_at IS NULL OR last_accessed_at < NOW() - INTERVAL '1 day' * ?)", skipAccessedWithinDays)).
		ExecContext(ctx)
	if err != nil {
		return 0, fmt.Errorf("drawer: decay importance: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("drawer: decay importance rows affected: %w", err)
	}
	return int(n), nil
}

func (r *Postgres) PruneLowImportance(ctx context.Context, sess database.SessionRunner, workspaceID string, threshold float64, minAccessCount, keepMin int) (int, error) {
	res, err := sess.DeleteFrom("memory_drawers").
		Where(dbr.Expr(
			`id IN (
			   SELECT id FROM memory_drawers
			   WHERE workspace_id = ? AND importance < ? AND access_count < ?
			   ORDER BY importance ASC
			   OFFSET ?
			 )`,
			workspaceID, threshold, minAccessCount, keepMin,
		)).
		ExecContext(ctx)
	if err != nil {
		return 0, fmt.Errorf("drawer: prune low importance: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("drawer: prune low importance rows affected: %w", err)
	}
	return int(n), nil
}

func (r *Postgres) GetByID(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string) (*memory.Drawer, error) {
	var d memory.Drawer
	err := sess.Select("id", "workspace_id", "wing", "room", "hall", "content",
		"importance", "memory_type", "source_file", "added_by", "added_by_agent",
		"state", "summary", "last_accessed_at", "access_count", "superseded_by",
		"cluster_id", "retry_count", "filed_at", "created_at").
		From("memory_drawers").
		Where("workspace_id = ? AND id = ?", workspaceID, drawerID).
		Where("superseded_by IS NULL").
		Where("state != 'merged'").
		LoadOneContext(ctx, &d)
	if err != nil {
		return nil, fmt.Errorf("drawer: get by id: %w", err)
	}
	return &d, nil
}

func (r *Postgres) BoostImportance(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string, delta, maxImportance float64) error {
	_, err := sess.InsertBySql(
		`UPDATE memory_drawers SET importance = LEAST(importance + ?, ?) WHERE workspace_id = ? AND id = ?`,
		delta, maxImportance, workspaceID, drawerID,
	).ExecContext(ctx)
	return err
}

func (r *Postgres) ActiveWorkspaces(ctx context.Context, sess database.SessionRunner, withinHours int) ([]string, error) {
	var ids []string
	_, err := sess.SelectBySql(
		`SELECT DISTINCT workspace_id FROM memory_drawers
		 WHERE created_at > NOW() - INTERVAL '1 hour' * ?
		    OR last_accessed_at > NOW() - INTERVAL '1 hour' * ?`,
		withinHours, withinHours,
	).LoadContext(ctx, &ids)
	if err != nil {
		return nil, fmt.Errorf("drawer: active workspaces: %w", err)
	}
	return ids, nil
}
