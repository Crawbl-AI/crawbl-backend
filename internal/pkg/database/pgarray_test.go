package database

import (
	"reflect"
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
		wantErr bool
	}{
		{name: "nil", in: nil, want: nil},
		{name: "empty", in: "{}", want: StringArray{}},
		{name: "simple bytes", in: []byte(`{"a","b"}`), want: StringArray{"a", "b"}},
		{name: "unquoted", in: "{foo,bar}", want: StringArray{"foo", "bar"}},
		{name: "with commas in quotes", in: `{"a,b","c"}`, want: StringArray{"a,b", "c"}},
		{name: "with escaped quote", in: `{"c\"d"}`, want: StringArray{`c"d`}},
		{name: "with escaped backslash", in: `{"a\\b"}`, want: StringArray{`a\b`}},
		{name: "unicode", in: `{"héllo","世界"}`, want: StringArray{"héllo", "世界"}},
		{name: "invalid missing braces", in: "not an array", wantErr: true},
		{name: "invalid unsupported type", in: 42, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got StringArray
			err := got.Scan(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Scan() error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Scan() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestStringArray_Roundtrip(t *testing.T) {
	// Anything we Serialize must Scan back to the same value.
	cases := []StringArray{
		{},
		{"simple"},
		{"a", "b", "c"},
		{"with,comma", "plain"},
		{`with"quote`, `with\backslash`},
		{"héllo", "世界", "mixed 🎉"},
		{"", "empty first"},
	}
	for _, c := range cases {
		t.Run("roundtrip", func(t *testing.T) {
			v, err := c.Value()
			if err != nil {
				t.Fatalf("Value() error = %v", err)
			}
			var got StringArray
			if err := got.Scan(v); err != nil {
				t.Fatalf("Scan() error = %v", err)
			}
			// Canonicalise nil to empty so the comparison is what callers care about.
			if got == nil {
				got = StringArray{}
			}
			want := c
			if want == nil {
				want = StringArray{}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("roundtrip mismatch: got %+v, want %+v", got, want)
			}
		})
	}
}
