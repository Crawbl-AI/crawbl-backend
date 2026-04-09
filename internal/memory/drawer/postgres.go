package drawer

import (
	"context"
	"fmt"

	"github.com/gocraft/dbr/v2"
	"github.com/pgvector/pgvector-go"

	"github.com/Crawbl-AI/crawbl-backend/internal/memory"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
)

type postgresRepo struct{}

// NewPostgres creates a new drawer Repo backed by PostgreSQL with pgvector.
func NewPostgres() Repo {
	return &postgresRepo{}
}

func (r *postgresRepo) Add(ctx context.Context, sess database.SessionRunner, d *memory.Drawer, embedding []float32) error {
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

func (r *postgresRepo) Delete(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string) error {
	_, err := sess.DeleteFrom("memory_drawers").
		Where("workspace_id = ? AND id = ?", workspaceID, drawerID).
		ExecContext(ctx)
	return err
}

func (r *postgresRepo) Search(ctx context.Context, sess database.SessionRunner, workspaceID string, queryEmbedding []float32, wing, room string, limit int) ([]memory.DrawerSearchResult, error) {
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

func (r *postgresRepo) CheckDuplicate(ctx context.Context, sess database.SessionRunner, workspaceID string, embedding []float32, threshold float64, limit int) ([]memory.DrawerSearchResult, error) {
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

func (r *postgresRepo) Count(ctx context.Context, sess database.SessionRunner, workspaceID string) (int, error) {
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

func (r *postgresRepo) ListWings(ctx context.Context, sess database.SessionRunner, workspaceID string) ([]memory.WingCount, error) {
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

func (r *postgresRepo) ListRooms(ctx context.Context, sess database.SessionRunner, workspaceID, wing string) ([]memory.RoomCount, error) {
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

func (r *postgresRepo) GetTopByImportance(ctx context.Context, sess database.SessionRunner, workspaceID, wing string, limit int) ([]memory.Drawer, error) {
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
	_, err := q.OrderDir("importance", false).
		Limit(uint64(limit)).
		LoadContext(ctx, &results)
	if err != nil {
		return nil, fmt.Errorf("drawer: top by importance: %w", err)
	}
	return results, nil
}

func (r *postgresRepo) ListByWorkspace(ctx context.Context, sess database.SessionRunner, workspaceID string, limit, offset int) ([]memory.Drawer, error) {
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

func (r *postgresRepo) GetByWingRoom(ctx context.Context, sess database.SessionRunner, workspaceID, wing, room string, limit int) ([]memory.Drawer, error) {
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
	_, err := q.Limit(uint64(limit)).
		LoadContext(ctx, &results)
	if err != nil {
		return nil, fmt.Errorf("drawer: get by wing/room: %w", err)
	}
	return results, nil
}

func (r *postgresRepo) AddIdempotent(ctx context.Context, sess database.SessionRunner, d *memory.Drawer, embedding []float32) error {
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

func (r *postgresRepo) ListByState(ctx context.Context, sess database.SessionRunner, workspaceID, state string, limit int) ([]memory.Drawer, error) {
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

func (r *postgresRepo) UpdateState(ctx context.Context, sess database.SessionRunner, drawerID, state string) error {
	_, err := sess.Update("memory_drawers").
		Set("state", state).
		Where("id = ?", drawerID).
		ExecContext(ctx)
	return err
}

func (r *postgresRepo) UpdateClassification(ctx context.Context, sess database.SessionRunner, drawerID, memoryType, summary, room string, importance float64) error {
	_, err := sess.Update("memory_drawers").
		Set("memory_type", memoryType).
		Set("summary", summary).
		Set("room", room).
		Set("importance", importance).
		Where("id = ?", drawerID).
		ExecContext(ctx)
	return err
}

func (r *postgresRepo) SetSupersededBy(ctx context.Context, sess database.SessionRunner, drawerID, supersededBy string) error {
	_, err := sess.Update("memory_drawers").
		Set("superseded_by", supersededBy).
		Where("id = ?", drawerID).
		ExecContext(ctx)
	return err
}

func (r *postgresRepo) SetClusterID(ctx context.Context, sess database.SessionRunner, drawerID, clusterID string) error {
	_, err := sess.Update("memory_drawers").
		Set("cluster_id", clusterID).
		Where("id = ?", drawerID).
		ExecContext(ctx)
	return err
}

func (r *postgresRepo) TouchAccess(ctx context.Context, sess database.SessionRunner, drawerID string) error {
	_, err := sess.Update("memory_drawers").
		Set("last_accessed_at", dbr.Expr("NOW()")).
		Set("access_count", dbr.Expr("access_count + 1")).
		Where("id = ?", drawerID).
		ExecContext(ctx)
	return err
}

func (r *postgresRepo) IncrementRetryCount(ctx context.Context, sess database.SessionRunner, drawerID string) error {
	_, err := sess.Update("memory_drawers").
		Set("retry_count", dbr.Expr("retry_count + 1")).
		Where("id = ?", drawerID).
		ExecContext(ctx)
	return err
}

func (r *postgresRepo) DecayImportance(ctx context.Context, sess database.SessionRunner, workspaceID string, olderThanDays, skipAccessedWithinDays int, factor, floor float64) (int, error) {
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

func (r *postgresRepo) PruneLowImportance(ctx context.Context, sess database.SessionRunner, workspaceID string, threshold float64, minAccessCount, keepMin int) (int, error) {
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

func (r *postgresRepo) GetByID(ctx context.Context, sess database.SessionRunner, workspaceID, drawerID string) (*memory.Drawer, error) {
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

func (r *postgresRepo) BoostImportance(ctx context.Context, sess database.SessionRunner, drawerID string, delta, maxImportance float64) error {
	_, err := sess.InsertBySql(
		`UPDATE memory_drawers SET importance = LEAST(importance + ?, ?) WHERE id = ?`,
		delta, maxImportance, drawerID,
	).ExecContext(ctx)
	return err
}

func (r *postgresRepo) ActiveWorkspaces(ctx context.Context, sess database.SessionRunner, withinHours int) ([]string, error) {
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
