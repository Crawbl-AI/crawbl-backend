// Package defaults holds shared default values used across the crawbl
// backend. Centralizing these makes tuning a single-point edit.
package defaults

import "time"

const (
	// ShortTimeout is the default timeout for quick operations: health
	// checks, simple DB pings, in-process lookups.
	ShortTimeout = 5 * time.Second

	// MediumTimeout is the default timeout for typical I/O operations:
	// single HTTP request, dbr query, gRPC unary call.
	MediumTimeout = 15 * time.Second

	// LongTimeout is the default timeout for heavyweight operations:
	// external LLM calls, file uploads, batch DB writes.
	LongTimeout = 30 * time.Second

	// ShutdownGracePeriod bounds how long we wait for in-flight work to
	// drain during a graceful shutdown.
	ShutdownGracePeriod = 10 * time.Second
)
