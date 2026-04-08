package drawer

import (
	"context"
	"fmt"

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
			`INSERT INTO memory_drawers (id, workspace_id, wing, room, hall, content, embedding, importance, memory_type, source_file, added_by, filed_at, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			d.ID, d.WorkspaceID, d.Wing, d.Room, d.Hall, d.Content, vec,
			d.Importance, d.MemoryType, d.SourceFile, d.AddedBy, d.FiledAt, d.CreatedAt,
		).ExecContext(ctx)
		return err
	}

	_, err = sess.InsertBySql(
		`INSERT INTO memory_drawers (id, workspace_id, wing, room, hall, content, importance, memory_type, source_file, added_by, filed_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.WorkspaceID, d.Wing, d.Room, d.Hall, d.Content,
		d.Importance, d.MemoryType, d.SourceFile, d.AddedBy, d.FiledAt, d.CreatedAt,
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

	where := "workspace_id = $2 AND embedding IS NOT NULL"
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
