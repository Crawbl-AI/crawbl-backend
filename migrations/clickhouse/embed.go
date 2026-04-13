// Package clickhousemigrations embeds the ClickHouse SQL migration files
// so they are compiled into the binary and available without filesystem access.
package clickhousemigrations

import "embed"

// FS contains all *.sql migration files for the ClickHouse analytics schema.
//
//go:embed *.sql
var FS embed.FS
