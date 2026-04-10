// Package database provides PostgreSQL database connection management,
// configuration, and transaction utilities for the Crawbl orchestrator.
// It wraps the dbr library with Crawbl-specific defaults and error handling.
package database

import (
	"time"

	"github.com/gocraft/dbr/v2"
)

// SessionRunner defines the interface for database session operations.
// It mirrors the dbr.Session interface methods needed for building queries.
// This interface allows repository code to work with either a session or a transaction,
// enabling clean transaction handling patterns.
type SessionRunner interface {
	// Select creates a SELECT statement for the given columns.
	Select(column ...any) *dbr.SelectStmt

	// SelectBySql creates a SELECT statement from raw SQL with optional parameters.
	SelectBySql(query string, value ...any) *dbr.SelectStmt

	// InsertInto creates an INSERT statement for the given table.
	InsertInto(table string) *dbr.InsertStmt

	// InsertBySql creates an INSERT statement from raw SQL with optional parameters.
	InsertBySql(query string, value ...any) *dbr.InsertStmt

	// Update creates an UPDATE statement for the given table.
	Update(table string) *dbr.UpdateStmt

	// DeleteFrom creates a DELETE statement for the given table.
	DeleteFrom(table string) *dbr.DeleteStmt
}

// Default database connection configuration values.
// These are used when environment variables are not set.
const (
	// DefaultHost is the default PostgreSQL server hostname.
	DefaultHost = "127.0.0.1"

	// DefaultPort is the default PostgreSQL server port.
	DefaultPort = "5432"

	// DefaultUser is the default database user name.
	DefaultUser = "postgres"

	// DefaultPassword is the default database password.
	DefaultPassword = "postgres"

	// DefaultName is the default database name.
	DefaultName = "crawbl"

	// DefaultSchema is the default schema to use for queries.
	DefaultSchema = "orchestrator"

	// DefaultSSLMode is the default SSL mode for connections.
	DefaultSSLMode = "disable"

	// DefaultMaxOpenConnections is the default maximum number of open connections.
	DefaultMaxOpenConnections = 20

	// DefaultMaxIdleConnections is the default maximum number of idle connections.
	DefaultMaxIdleConnections = 10

	// DefaultConnMaxLifetime is the default maximum lifetime of a connection.
	DefaultConnMaxLifetime = 5 * time.Minute

	// DefaultConnMaxIdleTime is the default maximum idle time for a connection.
	// Set below PostgreSQL's default idle-in-transaction timeout to avoid stale connections.
	DefaultConnMaxIdleTime = 2 * time.Minute

	// DefaultPingAttempts is the default number of attempts to ping the database on startup.
	DefaultPingAttempts = 5

	// DefaultPingDelay is the default delay between ping attempts.
	DefaultPingDelay = 2 * time.Second
)

// Config holds the database connection configuration.
// It contains all parameters needed to establish a PostgreSQL connection
// and configure the connection pool.
type Config struct {
	// Host is the PostgreSQL server hostname or IP address.
	Host string

	// Port is the PostgreSQL server port.
	Port string

	// User is the database user name for authentication.
	User string

	// Password is the database user password for authentication.
	Password string

	// Name is the database name to connect to.
	Name string

	// Schema is the schema to set as the default search path.
	Schema string

	// SSLMode controls whether SSL is used for the connection.
	// Common values: "disable", "require", "verify-ca", "verify-full".
	SSLMode string

	// MaxOpenConnections is the maximum number of open connections to the database.
	MaxOpenConnections int

	// MaxIdleConnections is the maximum number of connections in the idle connection pool.
	MaxIdleConnections int

	// ConnMaxLifetime is the maximum amount of time a connection may be reused.
	ConnMaxLifetime time.Duration

	// ConnMaxIdleTime is the maximum amount of time a connection may sit idle
	// before being closed. Should be less than the server-side idle timeout
	// (PostgreSQL default: 10 min) to avoid "unexpected EOF" errors.
	ConnMaxIdleTime time.Duration
}
