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

	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

// minBoostImportance is the lower bound for the importance score applied by
// BoostImportance / enrichment passes.
const minBoostImportance = 3.0

// Postgres is the memory_drawers repository backed by PostgreSQL with
// pgvector for embeddings. It implements repo.DrawerRepo; callers hold it
// through that interface.
type Postgres struct{}

// NewPostgres creates a new drawer repository backed by PostgreSQL with pgvector.
func NewPostgres() *Postgres {
	return &Postgres{}
}

// addDrawer is the shared implementation behind Add and AddIdempotent.
// If onConflictDoNothing is true, a duplicate row is silently ignored;
// otherwise a duplicate raises a constraint violation.
func (r *Postgres) addDrawer(ctx context.Context, sess database.SessionRunner, d *memory.Drawer, embedding []float32, onConflictDoNothing bool) error {
	// Defensive default: the memory_drawers.state column was added
	// with `DEFAULT 'raw'` but this INSERT explicitly supplies every
	// column, so an empty d.State would be persisted verbatim. An
	// empty state leaves the drawer invisible to memory_process
	// (indexed on state='raw'), silently dropping the note from the
	// cold pipeline. Any caller that forgot to set it lands in 'raw'.
	if d.State == "" {
		d.State = string(memory.DrawerStateRaw)
	}

	if len(embedding) > 0 {
		vec := pgvector.NewVector(embedding)
		if onConflictDoNothing {
			_, err := sess.InsertBySql(
				`INSERT INTO memory_drawers (id, workspace_id, wing, room, hall, content, embedding, importance, memory_type, source_file, added_by, added_by_agent, state, pipeline_tier, entity_count, triple_count, filed_at, created_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				 ON CONFLICT DO NOTHING`,
				d.ID, d.WorkspaceID, d.Wing, d.Room, d.Hall, d.Content, vec,
				d.Importance, d.MemoryType, d.SourceFile, d.AddedBy, d.AddedByAgent, d.State,
				d.PipelineTier, d.EntityCount, d.TripleCount, d.FiledAt, d.CreatedAt,
			).ExecContext(ctx)
			return err
		}
		_, err := sess.InsertInto("memory_drawers").
			Columns("id", "workspace_id", "wing", "room", "hall", "content", "embedding",
				"importance", "memory_type", "source_file", "added_by", "added_by_agent",
				"state", "pipeline_tier", "entity_count", "triple_count", "filed_at", "created_at").
			Values(d.ID, d.WorkspaceID, d.Wing, d.Room, d.Hall, d.Content, vec,
				d.Importance, d.MemoryType, d.SourceFile, d.AddedBy, d.AddedByAgent, d.State,
				d.PipelineTier, d.EntityCount, d.TripleCount, d.FiledAt, d.CreatedAt).
			ExecContext(ctx)
		return err
	}

	if onConflictDoNothing {
		_, err := sess.InsertBySql(
			`INSERT INTO memory_drawers (id, workspace_id, wing, room, hall, content, importance, memory_type, source_file, added_by, added_by_agent, state, pipeline_tier, entity_count, triple_count, filed_at, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT DO NOTHING`,
			d.ID, d.WorkspaceID, d.Wing, d.Room, d.Hall, d.Content,
			d.Importance, d.MemoryType, d.SourceFile, d.AddedBy, d.AddedByAgent, d.State,
			d.PipelineTier, d.EntityCount, d.TripleCount, d.FiledAt, d.CreatedAt,
		).ExecContext(ctx)
		return err
	}

	_, err := sess.InsertInto("memory_drawers").
		Columns("id", "workspace_id", "wing", "room", "hall", "content",
			"importance", "memory_type", "source_file", "added_by", "added_by_agent",
			"state", "pipeline_tier", "entity_count", "triple_count", "filed_at", "created_at").
		Values(d.ID, d.WorkspaceID, d.Wing, d.Room, d.Hall, d.Content,
			d.Importance, d.MemoryType, d.SourceFile, d.AddedBy, d.AddedByAgent, d.State,
			d.PipelineTier, d.EntityCount, d.TripleCount, d.FiledAt, d.CreatedAt).
		ExecContext(ctx)
	return err
}

func (r *Postgres) Add(ctx context.Context, sess database.SessionRunner, d *memory.Drawer, embedding []float32) error {
	count, err := r.Count(ctx, sess, d.WorkspaceID)
	if err != nil {
		return fmt.Errorf("drawer: check count: %w", err)
	}
	if count >= memory.MaxDrawersPerWorkspace {
		return fmt.Errorf("drawer: workspace limit reached (%d)", memory.MaxDrawersPerWorkspace)
	}
	return r.addDrawer(ctx, sess, d, embedding, false)
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

	// NOTE: $1 (the embedding) appears twice — in the similarity expression and
	// ORDER BY — so dbr's SelectBySql would raise "wrong placeholder count".
	// We use raw db.QueryContext instead (same approach as CheckDuplicate).
	// pgvector requires OPERATOR(public.<=>) and ::public.vector casts because
	// search_path is set to the orchestrator schema, not public.
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
		        1 - (embedding OPERATOR(public.<=>) $1::public.vector) AS similarity
		 FROM memory_drawers
		 WHERE %s
		 ORDER BY embedding OPERATOR(public.<=>) $1::public.vector
		 LIMIT $%d`,
		where, paramIdx,
	)

	db, ok := sess.(*dbr.Session)
	if !ok || db == nil || db.DB == nil {
		return nil, fmt.Errorf("drawer: search: session is not a *dbr.Session with a live connection")
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("drawer: search: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []memory.DrawerSearchResult
	for rows.Next() {
		var res memory.DrawerSearchResult
		if scanErr := rows.Scan(
			&res.ID, &res.WorkspaceID, &res.Wing, &res.Room, &res.Hall, &res.Content,
			&res.Importance, &res.MemoryType, &res.SourceFile, &res.AddedBy,
			&res.FiledAt, &res.CreatedAt, &res.State, &res.Summary, &res.AddedByAgent,
			&res.Similarity,
		); scanErr != nil {
			return nil, fmt.Errorf("drawer: search scan: %w", scanErr)
		}
		results = append(results, res)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("drawer: search iterate: %w", rowsErr)
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
    SELECT id, 1 - (embedding OPERATOR(public.<=>) $1::public.vector) AS similarity, false AS via_kg
    FROM memory_drawers
    WHERE workspace_id = $2
      AND embedding IS NOT NULL
      AND (state IN ('raw', 'processed') OR state = '')
      AND superseded_by IS NULL
    ORDER BY embedding OPERATOR(public.<=>) $1::public.vector
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

	// NOTE: hybridSearchSQL reuses $1 twice and $2 three times, so dbr's
	// SelectBySql raises "wrong placeholder count". Use raw db.QueryContext
	// instead (same pattern as CheckDuplicate).
	db, ok := sess.(*dbr.Session)
	if !ok || db == nil || db.DB == nil {
		return nil, fmt.Errorf("drawerrepo: hybrid search: session is not a *dbr.Session with a live connection")
	}
	rows, err := db.QueryContext(ctx, hybridSearchSQL, vec, workspaceID, vectorLimit, pq.Array(terms), limit)
	if err != nil {
		return nil, fmt.Errorf("drawerrepo: hybrid search: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []memory.HybridSearchResult
	for rows.Next() {
		var res memory.HybridSearchResult
		if scanErr := rows.Scan(
			&res.ID, &res.WorkspaceID, &res.Wing, &res.Room, &res.Hall, &res.Content,
			&res.Importance, &res.MemoryType, &res.SourceFile, &res.AddedBy, &res.AddedByAgent,
			&res.FiledAt, &res.CreatedAt, &res.State, &res.Summary, &res.LastAccessedAt,
			&res.AccessCount, &res.SupersededBy, &res.ClusterID, &res.RetryCount,
			&res.Similarity, &res.ViaKG,
		); scanErr != nil {
			return nil, fmt.Errorf("drawerrepo: hybrid search scan: %w", scanErr)
		}
		results = append(results, res)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("drawerrepo: hybrid search iterate: %w", rowsErr)
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
	// NOTE: CheckDuplicate reuses the same $1 placeholder three times
	// (similarity select, threshold filter, ORDER BY). dbr's
	// SelectBySql rejects reused $N placeholders with "wrong placeholder
	// count" because it counts occurrences, and its "?" path drops the
	// pgvector OID so Postgres raises "vector <=> unknown". We sidestep
	// both bugs by running the query through the underlying
	// database/sql driver directly — lib/pq knows how to pass pgvector
	// values and the $N placeholder reuse is native Postgres.
	//
	// Three pgvector type headaches to be aware of:
	//
	//  1. pgvector.Vector.Value() emits a text literal that Postgres
	//     types as "unknown" unless we cast — so every $1 usage needs
	//     an explicit cast.
	//  2. The `vector` type lives in the `public` schema and our
	//     connection's search_path is `orchestrator`, so we MUST
	//     qualify as `public.vector` or Postgres raises
	//     "type vector does not exist".
	//  3. pgvector registers the `<=>` operator on the unqualified
	//     `vector` type family; with `search_path=orchestrator`
	//     Postgres cannot resolve it implicitly and raises
	//     "operator does not exist: public.vector <=> public.vector".
	//     We fix this by writing `OPERATOR(public.<=>)` so the
	//     operator is fully qualified at each call site.
	const query = `SELECT id, workspace_id, wing, room, hall, content, importance, memory_type,
	                      source_file, added_by, filed_at, created_at,
	                      state, summary, added_by_agent,
	                      1 - (embedding OPERATOR(public.<=>) $1::public.vector) AS similarity
	               FROM memory_drawers
	               WHERE workspace_id = $2 AND embedding IS NOT NULL
	                 AND 1 - (embedding OPERATOR(public.<=>) $1::public.vector) >= $3
	               ORDER BY embedding OPERATOR(public.<=>) $1::public.vector
	               LIMIT $4`

	db, ok := sess.(*dbr.Session)
	if !ok || db == nil || db.DB == nil {
		return nil, fmt.Errorf("drawer: check duplicate: session is not a *dbr.Session with a live connection")
	}
	rows, err := db.QueryContext(ctx, query, vec, workspaceID, threshold, limit)
	if err != nil {
		return nil, fmt.Errorf("drawer: check duplicate: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []memory.DrawerSearchResult
	for rows.Next() {
		var r memory.DrawerSearchResult
		if scanErr := rows.Scan(
			&r.ID, &r.WorkspaceID, &r.Wing, &r.Room, &r.Hall, &r.Content,
			&r.Importance, &r.MemoryType, &r.SourceFile, &r.AddedBy,
			&r.FiledAt, &r.CreatedAt, &r.State, &r.Summary, &r.AddedByAgent,
			&r.Similarity,
		); scanErr != nil {
			return nil, fmt.Errorf("drawer: check duplicate scan: %w", scanErr)
		}
		results = append(results, r)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("drawer: check duplicate iterate: %w", rowsErr)
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
	return r.addDrawer(ctx, sess, d, embedding, true)
}

func (r *Postgres) ListByState(ctx context.Context, sess database.SessionRunner, workspaceID, state string, limit int) ([]memory.Drawer, error) {
	if limit <= 0 {
		limit = 10
	}
	var rows []memory.Drawer
	// FOR UPDATE SKIP LOCKED is a Postgres locking clause that dbr has no builder
	// support for; raw SQL is required to avoid double-processing in concurrent workers.
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

// UpdateEmbedding persists a freshly generated embedding vector for a drawer.
// Called by the cold worker after classification so vector search can find the drawer.
func (r *Postgres) UpdateEmbedding(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string, embedding []float32) error {
	vec := pgvector.NewVector(embedding)
	_, err := sess.Update("memory_drawers").
		Set("embedding", vec).
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

// TouchAccessBatch updates last_accessed_at and increments access_count for
// all provided drawer IDs in a single UPDATE statement. This is the preferred
// path over calling TouchAccess in a loop — one round-trip means partial
// failure cannot skew retrieval ranking.
func (r *Postgres) TouchAccessBatch(ctx context.Context, sess database.SessionRunner, workspaceID string, drawerIDs []string) error {
	if len(drawerIDs) == 0 {
		return nil
	}
	_, err := sess.Update("memory_drawers").
		Set("last_accessed_at", dbr.Expr("NOW()")).
		Set("access_count", dbr.Expr("access_count + 1")).
		Where("workspace_id = ?", workspaceID).
		Where("id IN ?", drawerIDs).
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
	// ORDER BY importance DESC with OFFSET keepMin skips the top-keepMin rows
	// (the ones to keep) and targets the remainder — the lowest-importance drawers —
	// for deletion. The previous ASC ordering was inverted and deleted the
	// slightly-better drawers instead.
	res, err := sess.DeleteFrom("memory_drawers").
		Where(dbr.Expr(
			`id IN (
			   SELECT id FROM memory_drawers
			   WHERE workspace_id = ? AND importance < ? AND access_count < ?
			   ORDER BY importance DESC
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
	_, err := sess.Update("memory_drawers").
		Set("importance", dbr.Expr("LEAST(importance + ?, ?)", delta, maxImportance)).
		Where("workspace_id = ? AND id = ?", workspaceID, drawerID).
		ExecContext(ctx)
	return err
}

func (r *Postgres) ActiveWorkspaces(ctx context.Context, sess database.SessionRunner, withinHours int) ([]string, error) {
	var ids []string
	_, err := sess.Select("DISTINCT workspace_id").
		From("memory_drawers").
		Where(dbr.Or(
			dbr.Expr("created_at > NOW() - INTERVAL '1 hour' * ?", withinHours),
			dbr.Expr("last_accessed_at > NOW() - INTERVAL '1 hour' * ?", withinHours),
		)).
		LoadContext(ctx, &ids)
	if err != nil {
		return nil, fmt.Errorf("drawer: active workspaces: %w", err)
	}
	return ids, nil
}

// ListEnrichCandidates returns drawers eligible for asynchronous entity
// enrichment. The WHERE clause matches idx_drawers_enrich exactly so the
// partial index can serve the query cheaply.
func (r *Postgres) ListEnrichCandidates(ctx context.Context, sess database.SessionRunner, limit int) ([]memory.Drawer, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []memory.Drawer
	_, err := sess.Select("id", "workspace_id", "wing", "room", "hall", "content", "importance", "memory_type",
		"source_file", "added_by", "added_by_agent", "state", "summary",
		"pipeline_tier", "entity_count", "triple_count", "filed_at", "created_at").
		From("memory_drawers").
		Where("state = ?", "processed").
		Where("pipeline_tier <> ?", "llm").
		Where("entity_count = ?", 0).
		Where("importance >= ?", minBoostImportance).
		OrderBy("created_at ASC").
		Limit(uint64(limit)).
		LoadContext(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("drawer: list enrich candidates: %w", err)
	}
	return rows, nil
}

// ListCentroidTrainingSamples returns up to topN LLM-labelled drawers
// per memory type within the last windowDays, ordered by importance
// then recency. Used by the weekly centroid recompute job — this
// keeps the raw SQL and pgvector.Vector binding inside the repo so
// the job layer can stay driver-agnostic.
func (r *Postgres) ListCentroidTrainingSamples(ctx context.Context, sess database.SessionRunner, windowDays, topN int) ([]memory.CentroidTrainingSample, error) {
	if windowDays <= 0 {
		windowDays = 90
	}
	if topN <= 0 {
		topN = 500
	}
	type row struct {
		ID         string          `db:"id"`
		MemoryType string          `db:"memory_type"`
		Embedding  pgvector.Vector `db:"embedding"`
	}
	// ROW_NUMBER() window function with PARTITION BY require raw SQL;
	// dbr has no builder support for window functions. Uses ? placeholders
	// (not $N) because dbr's SelectBySql miscounts $N reuse.
	var rows []row
	_, err := sess.SelectBySql(
		`SELECT id, memory_type, embedding FROM (
		    SELECT id, memory_type, embedding,
		           ROW_NUMBER() OVER (
		               PARTITION BY memory_type
		               ORDER BY importance DESC, filed_at DESC
		           ) AS rnk
		    FROM memory_drawers
		    WHERE state = 'processed'
		      AND pipeline_tier = 'llm'
		      AND embedding IS NOT NULL
		      AND memory_type <> ''
		      AND created_at > NOW() - INTERVAL '1 day' * ?
		 ) ranked
		 WHERE rnk <= ?`,
		windowDays, topN,
	).LoadContext(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("drawer: list centroid training samples: %w", err)
	}
	out := make([]memory.CentroidTrainingSample, 0, len(rows))
	for i := range rows {
		out = append(out, memory.CentroidTrainingSample{
			ID:         rows[i].ID,
			MemoryType: rows[i].MemoryType,
			Embedding:  rows[i].Embedding.Slice(),
		})
	}
	return out, nil
}

// UpdateEnrichment sets entity_count / triple_count for a drawer after
// the enrichment worker has linked its KG nodes in.
func (r *Postgres) UpdateEnrichment(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string, entityCount, tripleCount int) error {
	_, err := sess.Update("memory_drawers").
		Set("entity_count", entityCount).
		Set("triple_count", tripleCount).
		Where("workspace_id = ? AND id = ?", workspaceID, drawerID).
		ExecContext(ctx)
	if err != nil {
		return fmt.Errorf("drawer: update enrichment: %w", err)
	}
	return nil
}
