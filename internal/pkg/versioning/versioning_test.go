package versioning

import "testing"

func TestIsGlobalSemverTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tag  string
		want bool
	}{
		// Valid global tags.
		{name: "plain v0.0.0", tag: "v0.0.0", want: true},
		{name: "plain v1.2.3", tag: "v1.2.3", want: true},
		{name: "plain v10.20.30", tag: "v10.20.30", want: true},
		{name: "pre-release suffix", tag: "v1.2.3-rc.1", want: true},
		{name: "build metadata", tag: "v1.2.3+sha.abcdef", want: true},
		{name: "pre + build", tag: "v1.2.3-beta.1+sha.abcdef", want: true},

		// Prefixed namespaces — the auth-filter / agent-runtime bug we
		// are guarding against. These must fall through to the
		// pattern-scoped git ls-remote path in CalculateForRepo.
		{name: "auth-filter prefix", tag: "auth-filter/v0.1.0", want: false},
		{name: "agent-runtime prefix", tag: "agent-runtime/v2.3.4", want: false},
		{name: "deep prefix", tag: "vendor/foo/v1.2.3", want: false},

		// Malformed / edge cases.
		{name: "empty string", tag: "", want: false},
		{name: "missing v prefix", tag: "1.2.3", want: false},
		{name: "missing patch", tag: "v1.2", want: false},
		{name: "trailing dot", tag: "v1.2.3.", want: false},
		{name: "word only", tag: "vlatest", want: false},
		{name: "crawbl fork suffix (not a global tag)", tag: "v0.6.8-crawbl.1", want: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isGlobalSemverTag(tc.tag); got != tc.want {
				t.Fatalf("isGlobalSemverTag(%q) = %v, want %v", tc.tag, got, tc.want)
			}
		})
	}
}
