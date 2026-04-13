// Package river wraps the riverqueue/river Postgres job queue library with
// crawbl-specific driver choice (database/sql via riverdatabasesql), schema
// migration helpers, and graceful shutdown. Memory-domain-specific job args
// and worker implementations live in internal/memory/background; this
// package is infrastructure only.
package river

import (
	"database/sql"
	"fmt"

	upstream "github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
)

// Client is a type alias for the upstream River client parameterized with
// the standard library transaction type. We use the database/sql driver so
// the existing dbr+pgx/v5 connection pool can be shared with River.
type Client = upstream.Client[*sql.Tx]

// Config is a type alias for the upstream River config. Exposed so that
// domain packages (e.g. internal/memory/background) can construct configs
// without importing the upstream package.
type Config = upstream.Config

// New constructs a River client bound to the given *sql.DB via the
// riverdatabasesql driver. The same *sql.DB may be shared with other
// consumers (dbr sessions); River acquires connections on demand.
func New(db *sql.DB, cfg *upstream.Config) (*Client, error) {
	client, err := upstream.NewClient(riverdatabasesql.New(db), cfg)
	if err != nil {
		return nil, fmt.Errorf("new river client: %w", err)
	}
	return client, nil
}
