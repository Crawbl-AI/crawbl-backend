// Package queue owns every River-backed background job, periodic schedule,
// and outbound event publisher used by the orchestrator.
package queue

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"sort"
	"strings"

	_ "github.com/ClickHouse/clickhouse-go/v2" // clickhouse driver registration
)

// OpenClickhouse reads CRAWBL_CLICKHOUSE_DSN (plus optional CRAWBL_CLICKHOUSE_USER
// and CRAWBL_CLICKHOUSE_PASSWORD) from the environment and opens a
// *sql.DB against the analytics database. Returns (nil, nil) when
// CRAWBL_CLICKHOUSE_DSN is unset — callers should treat that as
// "analytics disabled" and gracefully no-op their writer paths.
func OpenClickhouse(ctx context.Context, logger *slog.Logger) (*sql.DB, error) {
	if logger == nil {
		logger = slog.Default()
	}

	dsn := strings.TrimSpace(os.Getenv("CRAWBL_CLICKHOUSE_DSN"))
	if dsn == "" {
		logger.Warn("clickhouse disabled: CRAWBL_CLICKHOUSE_DSN not set — analytics will not be persisted")
		return nil, nil
	}
	if password := os.Getenv("CRAWBL_CLICKHOUSE_PASSWORD"); password != "" && !strings.Contains(dsn, "@") {
		user := os.Getenv("CRAWBL_CLICKHOUSE_USER")
		if user == "" {
			user = "default"
		}
		dsn = strings.Replace(dsn, "clickhouse://", "clickhouse://"+user+":"+password+"@", 1)
	}

	// Enable async inserts so high-volume LLM usage writes are batched
	// server-side.
	if !strings.Contains(dsn, "async_insert") {
		if strings.Contains(dsn, "?") {
			dsn += "&async_insert=1&wait_for_async_insert=0"
		} else {
			dsn += "?async_insert=1&wait_for_async_insert=0"
		}
	}

	db, err := sql.Open("clickhouse", dsn)
	if err != nil {
		return nil, fmt.Errorf("clickhouse connect: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("clickhouse ping: %w", err)
	}
	logger.Info("clickhouse connected")
	return db, nil
}

// MigrateClickhouse reads all *.sql files from the provided embed.FS and
// executes them sequentially against the ClickHouse database. Files are
// sorted lexicographically so numbered prefixes (001_, 002_, …) control
// order. Each file uses CREATE … IF NOT EXISTS so re-running is idempotent.
// Pass nil for db to no-op (analytics disabled).
func MigrateClickhouse(ctx context.Context, db *sql.DB, migrations fs.FS, logger *slog.Logger) error {
	if db == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}

	entries, err := fs.Glob(migrations, "*.sql")
	if err != nil {
		return fmt.Errorf("clickhouse migrate: glob: %w", err)
	}
	sort.Strings(entries)

	for _, name := range entries {
		ddl, err := fs.ReadFile(migrations, name)
		if err != nil {
			return fmt.Errorf("clickhouse migrate: read %s: %w", name, err)
		}
		// Split on semicolons — ClickHouse does not support multi-statement execution.
		for _, stmt := range strings.Split(string(ddl), ";") {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("clickhouse migrate: exec %s: %w", name, err)
			}
		}
		logger.Info("clickhouse migration applied", "file", name)
	}
	return nil
}
