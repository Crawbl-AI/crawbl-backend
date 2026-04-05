package server

import "encoding/json"

// jsonMarshal is a tiny wrapper so converse.go can call a single
// marshal helper without importing encoding/json directly from the
// hot path. Kept separate so future switch to a faster marshaler
// (sonic, ffjson) is a one-file change.
func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}
