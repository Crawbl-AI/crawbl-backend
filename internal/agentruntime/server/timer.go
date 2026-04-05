package server

import "time"

// timerAfter wraps time.After for dependency injection in tests. Non-exported
// to keep the server package surface minimal.
func timerAfter(d time.Duration) <-chan time.Time {
	return time.After(d)
}
