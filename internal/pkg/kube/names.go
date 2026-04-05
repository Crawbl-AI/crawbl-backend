// Package kube provides generic Kubernetes utilities reusable across the platform.
//
// This package contains helpers that are not tied to any specific CRD or domain:
//
//	names.go     — Resource name truncation with hash suffixes, deterministic checksums
//	builders.go  — Common K8s struct builders (TypeMeta, SecurityContext, EnvFrom, etc.)
//	ptr.go       — Generic pointer helper
package kube

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Name truncation
// ---------------------------------------------------------------------------

// TruncateName safely shortens a Kubernetes resource name to fit within maxLen.
//
// If the name already fits, it is returned lowercased and trimmed.
// If it exceeds maxLen, a hash suffix is appended to preserve uniqueness:
//
//	"workspace-very-long-workspace-id-that-exceeds-limits" (65 chars)
//	→ "workspace-very-long-workspace-id-that-exc-a1b2c3d4e5" (52 chars)
//
// This prevents collisions when two long names share a common prefix.
func TruncateName(name string, maxLen int) string {
	n := strings.ToLower(strings.Trim(name, "-"))
	if n == "" {
		return "resource"
	}
	if len(n) <= maxLen {
		return n
	}

	hash := ChecksumString(n)
	if len(hash) > hashLen {
		hash = hash[:hashLen]
	}

	keepLen := maxLen - len(hash) - 1 // -1 for the separator dash
	if keepLen < 1 {
		if len(hash) > maxLen {
			return hash[:maxLen]
		}
		return hash
	}

	prefix := strings.Trim(n[:keepLen], "-")
	if prefix == "" {
		return hash
	}
	return fmt.Sprintf("%s-%s", prefix, hash)
}

// ---------------------------------------------------------------------------
// Checksums (for config-change detection)
// ---------------------------------------------------------------------------

// ChecksumString returns a hex-encoded SHA-256 hash of s.
// Used in pod template annotations to trigger rolling updates when config changes.
func ChecksumString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// ChecksumMap returns a deterministic SHA-256 checksum of a string map.
// Keys are sorted first so the result is stable regardless of Go's random
// map iteration order. Used for checksumming ConfigMap data.
func ChecksumMap(m map[string]string) string {
	if len(m) == 0 {
		return ChecksumString("")
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(m[k])
		b.WriteByte('\n')
	}
	return ChecksumString(b.String())
}
