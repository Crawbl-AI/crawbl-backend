// Package river wraps the riverqueue/river Postgres job queue library with
// crawbl-specific driver choice (database/sql via riverdatabasesql), schema
// migration helpers, and graceful shutdown. Memory-domain-specific job args
// and worker implementations live in internal/memory/background; this
// package is infrastructure only.
package river

import (
	"database/sql"
	"time"

	upstream "github.com/riverqueue/river"
)

const (
	// defaultSoftStopTimeout is the budget for in-flight jobs to drain
	// naturally before we escalate to cancellation.
	defaultSoftStopTimeout = 20 * time.Second
	// defaultHardStopTimeout is the budget for StopAndCancel to force
	// cancellation of stuck jobs before the process exits.
	defaultHardStopTimeout = 10 * time.Second
)

// Client is a type alias for the upstream River client parameterized with
// the standard library transaction type. We use the database/sql driver so
// the existing dbr+pgx/v5 connection pool can be shared with River.
type Client = upstream.Client[*sql.Tx]

// Config is a type alias for the upstream River config. Exposed so that
// domain packages (e.g. internal/memory/background) can construct configs
// without importing the upstream package.
type Config = upstream.Config
