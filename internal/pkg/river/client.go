package river

import (
	"database/sql"
	"fmt"

	upstream "github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
)

// New constructs a River client bound to the given *sql.DB via the
// riverdatabasesql driver. The same *sql.DB may be shared with other
// consumers (dbr sessions); River acquires connections on demand.
//
// A nil cfg is allowed for publish-only callers (e.g. the API component
// that enqueues jobs but does not process them). River requires a non-nil
// config, so we substitute an empty one in that case.
func New(db *sql.DB, cfg *upstream.Config) (*Client, error) {
	if cfg == nil {
		cfg = &upstream.Config{}
	}
	client, err := upstream.NewClient(riverdatabasesql.New(db), cfg)
	if err != nil {
		return nil, fmt.Errorf("new river client: %w", err)
	}
	return client, nil
}
