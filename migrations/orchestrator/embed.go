// Package orchestratormigrations embeds the orchestrator SQL migration files
// so they are compiled into the binary and available without filesystem access.
package orchestratormigrations

import "embed"

// FS contains all *.sql migration files for the orchestrator schema.
//
//go:embed *.sql
var FS embed.FS
