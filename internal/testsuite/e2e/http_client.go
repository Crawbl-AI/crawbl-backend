// Package e2e — HTTP transport plumbing for step definitions.
// Every outbound request flows through doRequest which stamps
// tc.lastStatus + tc.lastBody for downstream assertion steps.
package e2e

import (
	"strings"
	"time"
)

const (
	// maxBodyDisplayLen is the maximum number of characters shown when
	// truncating a response body in error messages.
	maxBodyDisplayLen = 200

	// asyncAssertTimeout is how long polling assertions wait for
	// async agent-side effects (memory, audit, delegation, etc.).
	asyncAssertTimeout = 30 * time.Second
)

// abbreviatedBody returns at most maxBodyDisplayLen characters of body
// as a trimmed string, suitable for embedding in error messages without
// flooding logs.
func abbreviatedBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > maxBodyDisplayLen {
		return text[:maxBodyDisplayLen]
	}
	return text
}
