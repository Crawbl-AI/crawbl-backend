package e2e

import (
	"fmt"
	"strings"
)

// Reporter implements httpexpect.Reporter for CLI (non-testing.T) usage.
// It collects assertion errors without aborting, allowing the runner to
// check results after each test case.
type Reporter struct {
	errors []string
}

// Errorf records an assertion failure. Called by httpexpect on assertion errors.
func (r *Reporter) Errorf(format string, args ...interface{}) {
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

// Reset clears all recorded errors. Call before each test case.
func (r *Reporter) Reset() {
	r.errors = nil
}

// Failed returns true if any assertions failed since the last Reset.
func (r *Reporter) Failed() bool {
	return len(r.errors) > 0
}

// Error returns the first recorded error message.
func (r *Reporter) Error() string {
	if len(r.errors) == 0 {
		return ""
	}
	// Return first error only — subsequent failures are usually cascading.
	msg := r.errors[0]
	// Trim verbose httpexpect output to the essential assertion message.
	if idx := strings.Index(msg, "expected"); idx >= 0 {
		return msg[idx:]
	}
	return msg
}
