package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/gocraft/dbr/v2"
)

// PostgresStore is the durable, Postgres-backed implementation of
// memory.Store. Rows live in the agent_memories table (migration
// 000004_agent_memories.up.sql) scoped by (workspace_id, key). Every
// method is safe for concurrent use — Postgres provides the locking
// via row-level writes, and the dbr connection pool handles
// concurrency on the client side.
//
// The store owns the underlying *dbr.Connection and closes it via
// Close(). main.go constructs the connection once at startup and hands
// it to NewPostgresStore; there is no global database handle inside
// the agentruntime package tree.
type PostgresStore struct {
	conn *dbr.Connection
	now  func() time.Time
}

// NewPostgresStore wraps an already-open *dbr.Connection as a
// memory.Store. Pass nil for now to use time.Now().UTC(); tests that
// need deterministic timestamps can inject a clock.
func NewPostgresStore(conn *dbr.Connection, now func() time.Time) *PostgresStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &PostgresStore{conn: conn, now: now}
}

// List returns entries for a workspace ordered by updated_at DESC
// (tiebreak on key ASC for determinism). Category filter and
// offset/limit pagination mirror the shape the gRPC handler expects
// so the wire contract stays stable across Store implementations.
func (s *PostgresStore) List(ctx context.Context, workspaceID string, filter ListFilter) ([]Entry, error) {
	if workspaceID == "" {
		return nil, ErrInvalidInput
	}
	if s == nil || s.conn == nil {
		return nil, errors.New("memory: postgres store not initialized")
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = DefaultListLimit
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	sess := s.conn.NewSession(nil)
	stmt := sess.Select("key", "content", "category", "created_at", "updated_at").
		From("agent_memories").
		Where("workspace_id = ?", workspaceID)
	if filter.Category != "" {
		stmt = stmt.Where("category = ?", filter.Category)
	}
	stmt = stmt.OrderDesc("updated_at").OrderAsc("key").Limit(uint64(limit)).Offset(uint64(offset))

	var rows []memoryRow
	if _, err := stmt.LoadContext(ctx, &rows); err != nil {
		return nil, fmt.Errorf("memory: list postgres rows: %w", err)
	}
	out := make([]Entry, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toEntry())
	}
	return out, nil
}

// Create inserts a new row or updates an existing one with the same
// (workspace_id, key). CreatedAt is preserved on conflict so repeated
// upserts retain the original creation timestamp.
func (s *PostgresStore) Create(ctx context.Context, workspaceID string, entry Entry) (Entry, error) {
	if workspaceID == "" || entry.Key == "" {
		return Entry{}, ErrInvalidInput
	}
	if s == nil || s.conn == nil {
		return Entry{}, errors.New("memory: postgres store not initialized")
	}

	now := s.now()
	const q = `
		INSERT INTO agent_memories (workspace_id, key, content, category, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $5)
		ON CONFLICT (workspace_id, key) DO UPDATE
		  SET content = EXCLUDED.content,
		      category = EXCLUDED.category,
		      updated_at = EXCLUDED.updated_at
		RETURNING created_at, updated_at
	`
	var createdAt, updatedAt time.Time
	row := s.conn.DB.QueryRowContext(ctx, q, workspaceID, entry.Key, entry.Content, entry.Category, now)
	if err := row.Scan(&createdAt, &updatedAt); err != nil {
		return Entry{}, fmt.Errorf("memory: upsert entry: %w", err)
	}
	entry.CreatedAt = createdAt
	entry.UpdatedAt = updatedAt
	return entry, nil
}

// Delete removes a single row. Returns ErrNotFound when zero rows are
// affected so the gRPC handler can return codes.NotFound.
func (s *PostgresStore) Delete(ctx context.Context, workspaceID, key string) error {
	if workspaceID == "" || key == "" {
		return ErrInvalidInput
	}
	if s == nil || s.conn == nil {
		return errors.New("memory: postgres store not initialized")
	}

	res, err := s.conn.DB.ExecContext(ctx,
		`DELETE FROM agent_memories WHERE workspace_id = $1 AND key = $2`,
		workspaceID, key,
	)
	if err != nil {
		return fmt.Errorf("memory: delete entry: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("memory: rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Close shuts down the underlying connection pool. Idempotent; safe to
// call multiple times because dbr.Connection wraps *sql.DB which
// itself tolerates redundant Close calls.
func (s *PostgresStore) Close() error {
	if s == nil || s.conn == nil {
		return nil
	}
	if err := s.conn.DB.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
		return err
	}
	return nil
}

// memoryRow is the on-the-wire shape dbr loads from agent_memories.
// Kept private because callers never see it — List() translates into
// the public Entry type before returning.
type memoryRow struct {
	Key       string    `db:"key"`
	Content   string    `db:"content"`
	Category  string    `db:"category"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

func (r memoryRow) toEntry() Entry {
	return Entry{
		Key:       r.Key,
		Content:   r.Content,
		Category:  r.Category,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}

// Compile-time interface assertion.
var _ Store = (*PostgresStore)(nil)
