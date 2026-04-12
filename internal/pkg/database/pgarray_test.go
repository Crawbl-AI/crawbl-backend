package database

import (
	"reflect"
	"strings"
	"testing"
)

func TestStringArray_Value(t *testing.T) {
	tests := []struct {
		name string
		in   StringArray
		want any
	}{
		{name: "nil", in: nil, want: nil},
		{name: "empty", in: StringArray{}, want: "{}"},
		{name: "simple", in: StringArray{"a", "b"}, want: `{"a","b"}`},
		{name: "with commas", in: StringArray{"a,b", "c"}, want: `{"a,b","c"}`},
		{name: "with quotes", in: StringArray{`c"d`}, want: `{"c\"d"}`},
		{name: "with backslash", in: StringArray{`a\b`}, want: `{"a\\b"}`},
		{name: "mixed", in: StringArray{`a,"b`, `c\d`}, want: `{"a,\"b","c\\d"}`},
		{name: "unicode", in: StringArray{"héllo", "世界"}, want: `{"héllo","世界"}`},
		{name: "with braces in content", in: StringArray{"{foo}", "{bar,baz}"}, want: `{"{foo}","{bar,baz}"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.in.Value()
			if err != nil {
				t.Fatalf("Value() error = %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Value() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStringArray_Scan(t *testing.T) {
	tests := []struct {
		name    string
		in      any
		want    StringArray
		wantErr string // substring to look for; empty means no error
	}{
		{name: "nil", in: nil, want: nil},
		{name: "empty", in: "{}", want: StringArray{}},
		{name: "simple bytes", in: []byte(`{"a","b"}`), want: StringArray{"a", "b"}},
		{name: "unquoted", in: "{foo,bar}", want: StringArray{"foo", "bar"}},
		{name: "unquoted with whitespace padding", in: "{foo, bar, baz}", want: StringArray{"foo", "bar", "baz"}},
		{name: "quoted with commas", in: `{"a,b","c"}`, want: StringArray{"a,b", "c"}},
		{name: "quoted with escaped quote", in: `{"c\"d"}`, want: StringArray{`c"d`}},
		{name: "quoted with escaped backslash", in: `{"a\\b"}`, want: StringArray{`a\b`}},
		{name: "unicode", in: `{"héllo","世界"}`, want: StringArray{"héllo", "世界"}},
		{name: "single empty string element", in: `{""}`, want: StringArray{""}},
		{name: "mixed quoted and unquoted", in: `{foo,"bar, baz"}`, want: StringArray{"foo", "bar, baz"}},

		// Errors: malformed input must fail loudly, not silently mangle data.
		{name: "missing braces", in: "not an array", wantErr: "must start"},
		{name: "unsupported scan type", in: 42, wantErr: "unsupported source"},
		{name: "NULL single element", in: "{NULL}", wantErr: "NULL elements"},
		{name: "NULL middle element", in: "{foo,NULL,bar}", wantErr: "NULL elements"},
		{name: "unquoted quote mid-element", in: `{abc"def}`, wantErr: "unexpected quote"},
		{name: "unterminated quote", in: `{"abc}`, wantErr: "unterminated"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got StringArray
			err := got.Scan(tc.in)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("Scan() expected error containing %q, got nil; parsed = %+v", tc.wantErr, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("Scan() error = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Scan() unexpected error = %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Scan() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestStringArray_Roundtrip ensures anything Value() serializes can be Scan()'d
// back to the same []string — this is the real-world invariant callers depend
// on for write-then-read paths.
func TestStringArray_Roundtrip(t *testing.T) {
	cases := []StringArray{
		{},
		{""},
		{"simple"},
		{"a", "b", "c"},
		{"with,comma", "plain"},
		{`with"quote`, `with\backslash`},
		{"héllo", "世界", "mixed 🎉"},
		{"", "empty first"},
		{"{braces}", "{nested,inside}"},
		{"NULL"}, // quoted NULL is a legitimate string, must survive roundtrip
	}
	for i, c := range cases {
		t.Run(c.String(i), func(t *testing.T) {
			v, err := c.Value()
			if err != nil {
				t.Fatalf("Value() error = %v", err)
			}
			var got StringArray
			if err := got.Scan(v); err != nil {
				t.Fatalf("Scan() error = %v; intermediate literal = %q", err, v)
			}
			if got == nil {
				got = StringArray{}
			}
			want := c
			if want == nil {
				want = StringArray{}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("roundtrip mismatch: got %+v, want %+v (literal %q)", got, want, v)
			}
		})
	}
}

// String renders a StringArray as a test-case identifier. Not used at runtime.
func (a StringArray) String(idx int) string {
	if len(a) == 0 {
		return "case-empty"
	}
	parts := make([]string, 0, len(a)+1)
	parts = append(parts, "case")
	for _, s := range a {
		if len(s) > 20 {
			s = s[:20] + "..."
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "-")
}
