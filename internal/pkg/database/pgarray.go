// Package database provides shared database utilities including connection
// management, error helpers, and custom SQL types.
package database

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
)

// StringArray is a []string that serializes to / deserializes from a Postgres
// text[] column. It replaces lib/pq's pq.StringArray and pq.Array([]string)
// helpers after the pgx/v5/stdlib driver migration.
type StringArray []string

// Value implements driver.Valuer for StringArray.
func (a StringArray) Value() (driver.Value, error) {
	if a == nil {
		return nil, nil
	}
	if len(a) == 0 {
		return "{}", nil
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, s := range a {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('"')
		for _, r := range s {
			switch r {
			case '\\':
				b.WriteString(`\\`)
			case '"':
				b.WriteString(`\"`)
			default:
				b.WriteRune(r)
			}
		}
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String(), nil
}

// Scan implements sql.Scanner for StringArray.
func (a *StringArray) Scan(src any) error {
	if src == nil {
		*a = nil
		return nil
	}
	var raw string
	switch v := src.(type) {
	case string:
		raw = v
	case []byte:
		raw = string(v)
	default:
		return fmt.Errorf("database.StringArray.Scan: unsupported source type %T", src)
	}
	parsed, err := parseTextArray(raw)
	if err != nil {
		return fmt.Errorf("database.StringArray.Scan: %w", err)
	}
	*a = parsed
	return nil
}

// parseTextArray parses a Postgres text array literal like:
//
//	{}              -> []
//	{foo,bar}       -> ["foo", "bar"]
//	{"a,b","c\"d"}  -> ["a,b", `c"d`]
//
// Only supports single-dimension arrays of text/varchar; nested arrays and
// NULL elements are NOT supported (callers currently never store those).
func parseTextArray(s string) ([]string, error) {
	if len(s) < 2 || s[0] != '{' || s[len(s)-1] != '}' {
		return nil, errors.New("invalid array literal: must start with '{' and end with '}'")
	}
	body := s[1 : len(s)-1]
	if body == "" {
		return []string{}, nil
	}
	var out []string
	var current strings.Builder
	inQuoted := false
	escaped := false
	for i := range len(body) {
		c := body[i]
		if escaped {
			current.WriteByte(c)
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if c == '"' {
			inQuoted = !inQuoted
			continue
		}
		if c == ',' && !inQuoted {
			out = append(out, current.String())
			current.Reset()
			continue
		}
		current.WriteByte(c)
	}
	out = append(out, current.String())
	return out, nil
}
