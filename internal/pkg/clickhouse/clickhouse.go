// Package clickhouse is a thin wrapper around the native ClickHouse
// database/sql driver. It centralizes connection setup, DSN credential
// injection, and health-checking so every service that needs an
// analytics connection uses the same shape.
//
// Only infrastructure concerns live here — no table-specific logic.
// Repositories in internal/orchestrator/repo/* own the actual queries.
package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strings"

	_ "github.com/ClickHouse/clickhouse-go/v2" // clickhouse driver registration
)

// Open reads CRAWBL_CLICKHOUSE_DSN (plus optional CRAWBL_CLICKHOUSE_USER
// and CRAWBL_CLICKHOUSE_PASSWORD) from the environment and opens a
// *sql.DB against the analytics database. Returns (nil, nil) when
// CRAWBL_CLICKHOUSE_DSN is unset — callers should treat that as
// "analytics disabled" and gracefully no-op their writer paths.
//
// The DSN format is the standard clickhouse-go v2 form:
//
//	clickhouse://[user:password@]host:port/database[?param=value]
//
// When the DSN has no embedded credentials and CRAWBL_CLICKHOUSE_PASSWORD
// is set, Open injects user:pass into the DSN before the host to keep
// credentials out of the config file.
func Open(ctx context.Context, logger *slog.Logger) (*sql.DB, error) {
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
