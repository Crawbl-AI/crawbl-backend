package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/gocraft/dbr/v2/dialect"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // pgx/v5 database/sql driver registration

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/configenv"
)

// ConfigFromEnv creates a Config from environment variables using the given prefix.
// Environment variables are expected in the format: {PREFIX}_DATABASE_{FIELD}.
// For example, with prefix "APP_", it looks for APP_DATABASE_HOST, APP_DATABASE_PORT, etc.
// Secret values (User, Password) are retrieved using configenv.SecretString for secure handling.
// If environment variables are not set, default values from types.go are used.
func ConfigFromEnv(prefix string) Config {
	return Config{
		Host:               configenv.StringOr(prefix+"DATABASE_HOST", DefaultHost),
		Port:               configenv.StringOr(prefix+"DATABASE_PORT", DefaultPort),
		User:               configenv.SecretString(prefix+"DATABASE_USER", DefaultUser),
		Password:           configenv.SecretString(prefix+"DATABASE_PASSWORD", DefaultPassword),
		Name:               configenv.StringOr(prefix+"DATABASE_NAME", DefaultName),
		Schema:             configenv.StringOr(prefix+"DATABASE_SCHEMA", DefaultSchema),
		SSLMode:            configenv.StringOr(prefix+"DATABASE_SSLMODE", DefaultSSLMode),
		MaxOpenConnections: configenv.IntOr(prefix+"DATABASE_MAX_OPEN_CONNECTIONS", DefaultMaxOpenConnections),
		MaxIdleConnections: configenv.IntOr(prefix+"DATABASE_MAX_IDLE_CONNECTIONS", DefaultMaxIdleConnections),
		ConnMaxLifetime:    configenv.DurationOr(prefix+"DATABASE_CONN_MAX_LIFETIME", DefaultConnMaxLifetime),
		ConnMaxIdleTime:    configenv.DurationOr(prefix+"DATABASE_CONN_MAX_IDLE_TIME", DefaultConnMaxIdleTime),
	}
}

// New creates a new database connection using the provided configuration.
// It opens a PostgreSQL connection, configures the connection pool settings,
// and verifies connectivity with retry logic before returning the connection.
//
// The connection is wrapped with dbr for query building and uses the PostgreSQL dialect.
//
// Returns:
//   - A dbr.Connection ready for use with the PostgreSQL dialect.
//   - An error if the connection cannot be established or pinged.
func New(config Config) (*dbr.Connection, error) {
	db, err := sql.Open("pgx", buildDriverDSN(config, true))
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(config.MaxOpenConnections)
	db.SetMaxIdleConns(config.MaxIdleConnections)
	db.SetConnMaxLifetime(config.ConnMaxLifetime)
	db.SetConnMaxIdleTime(config.ConnMaxIdleTime)

	if err := pingWithRetry(context.Background(), db, DefaultPingAttempts, DefaultPingDelay); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return &dbr.Connection{
		DB:            db,
		Dialect:       dialect.PostgreSQL,
		EventReceiver: &dbr.NullEventReceiver{},
	}, nil
}

// EnsureSchema creates the configured schema if it does not already exist.
// This is useful for bootstrapping new database instances where the schema
// needs to be created before migrations run.
// If the schema configuration is empty or whitespace, this function returns immediately.
//
// Returns:
//   - An error if the connection fails or the schema creation fails.
//   - nil if the schema already exists or was created successfully.
func EnsureSchema(config Config) error {
	if strings.TrimSpace(config.Schema) == "" {
		return nil
	}

	db, err := sql.Open("pgx", BuildDSN(config, false))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if err := pingWithRetry(context.Background(), db, DefaultPingAttempts, DefaultPingDelay); err != nil {
		return fmt.Errorf("ping db for schema bootstrap: %w", err)
	}

	_, err = db.ExecContext(context.Background(),
		"CREATE SCHEMA IF NOT EXISTS "+pgx.Identifier{config.Schema}.Sanitize())
	return err
}

// BuildDSN constructs a PostgreSQL connection string (DSN) in URL format.
// The DSN includes host, port, credentials, database name, and SSL mode.
// When includeSchema is true and a schema is configured, it adds search_path
// to set the default schema for the connection.
//
// This format is suitable for logging and debugging, but use buildDriverDSN
// for actual driver connections as some drivers prefer the space-separated format.
func BuildDSN(config Config, includeSchema bool) string {
	dsnURL := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(config.User, config.Password),
		Host:   net.JoinHostPort(config.Host, config.Port),
		Path:   "/" + config.Name,
	}

	query := url.Values{}
	query.Set("sslmode", config.SSLMode)
	if includeSchema && strings.TrimSpace(config.Schema) != "" {
		query.Set("search_path", config.Schema)
	}
	dsnURL.RawQuery = query.Encode()

	return dsnURL.String()
}

// IsRecordExistsError checks if the given error indicates a unique constraint violation
// (PostgreSQL error code 23505). This is returned when attempting to insert a record
// that would violate a unique constraint or primary key constraint.
//
// Returns:
//   - true if the error is a PostgreSQL unique violation error.
//   - false if the error is nil or a different type of error.
func IsRecordExistsError(err error) bool {
	if err == nil {
		return false
	}

	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// IsRecordNotFoundError checks if the given error indicates that no record was found.
// This includes dbr.ErrNotFound and sql.ErrNoRows, which are returned when a query
// expected to return rows returns none.
//
// Returns:
//   - true if the error indicates no records were found.
//   - false if the error is nil or indicates a different condition.
func IsRecordNotFoundError(err error) bool {
	if err == nil {
		return false
	}

	return errors.Is(err, dbr.ErrNotFound) || errors.Is(err, sql.ErrNoRows)
}

// pingWithRetry attempts to ping the database multiple times with a delay between attempts.
// This is useful during startup when the database may not be immediately available,
// such as in containerized environments where services start concurrently.
//
// Returns:
//   - nil on successful ping.
//   - The last error encountered if all attempts fail.
//   - ctx.Err() if the context is cancelled during a retry delay.
func pingWithRetry(ctx context.Context, db *sql.DB, attempts int, delay time.Duration) error {
	var lastErr error

	for range attempts {
		if err := db.PingContext(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return lastErr
}

// buildDriverDSN constructs a PostgreSQL connection string in URL format accepted
// by pgx/v5. Using url.UserPassword ensures special characters in the password
// (spaces, @, /, etc.) are percent-encoded and cannot break the DSN parsing.
func buildDriverDSN(config Config, includeSchema bool) string {
	return BuildDSN(config, includeSchema)
}
