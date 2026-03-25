// Package database provides PostgreSQL database connection management,
// configuration, and transaction utilities for the Crawbl orchestrator.
// It wraps the dbr library with Crawbl-specific defaults and error handling.
package database

import "time"

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
}
