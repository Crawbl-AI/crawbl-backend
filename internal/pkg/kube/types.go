package kube

// ---------------------------------------------------------------------------
// Name length limits
// ---------------------------------------------------------------------------

const (
	// MaxNameLen is the general Kubernetes object name limit (63 characters).
	MaxNameLen = 63

	// MaxWorkloadNameLen is the limit for StatefulSet and Deployment names.
	// These resources auto-generate pod names with suffixes, so the base name
	// needs headroom to stay under the 63-char label value limit.
	MaxWorkloadNameLen = 52

	// hashLen is how many characters of the SHA-256 hash to keep when truncating.
	hashLen = 10
)
