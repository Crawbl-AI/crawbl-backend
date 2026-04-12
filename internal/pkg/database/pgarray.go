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
// text[] column via the database/sql driver.Valuer / sql.Scanner interfaces.
//
// The pgx/v5/stdlib driver delivers text[] as []byte; Scan accepts both
// []byte and string for portability. NULL array elements are rejected
// (returns an error) rather than silently coerced to the literal string
// "NULL" — callers should ensure their columns are NOT NULL or use a
// dedicated nullable type if true nullability is required.
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
		return fmt.Errorf("scan string array: unsupported source type %T", src)
	}
	parsed, err := parseTextArray(raw)
	if err != nil {
		return fmt.Errorf("scan string array: %w", err)
	}
	*a = parsed
	return nil
}

// parseTextArray parses a Postgres text array literal into a []string.
// Supports the format Postgres emits for text[] columns:
//
//	{}                     -> []
//	{foo,bar}              -> ["foo", "bar"]           (unquoted, trimmed)
//	{"a,b","c\"d"}         -> ["a,b", `c"d`]           (quoted, escapes honoured)
//
// Rejects (with an explicit error) inputs this codebase does not expect:
//   - NULL elements (unquoted literal NULL between commas)
//   - unquoted elements containing a bare double-quote
//   - malformed outer braces
//
// Nested arrays are not supported.
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
	quoted := false   // currently inside a quoted element
	sawQuote := false // the element (past or present) began with a quote
	escaped := false  // the previous char was a backslash escape
	elementStarted := false

	flush := func() error {
		raw := current.String()
		current.Reset()
		if !sawQuote {
			trimmed := strings.TrimSpace(raw)
			if trimmed == "NULL" {
				return errors.New("NULL elements are not supported")
			}
			out = append(out, trimmed)
		} else {
			out = append(out, raw)
		}
		sawQuote = false
		elementStarted = false
		return nil
	}

	for i := range len(body) {
		c := body[i]
		if escaped {
			current.WriteByte(c)
			escaped = false
			elementStarted = true
			continue
		}
		if quoted {
			switch c {
			case '\\':
				escaped = true
			case '"':
				quoted = false
			default:
				current.WriteByte(c)
			}
			continue
		}
		// unquoted
		switch c {
		case '"':
			if elementStarted {
				return nil, fmt.Errorf("unexpected quote in unquoted element at byte %d", i)
			}
			quoted = true
			sawQuote = true
			elementStarted = true
		case ',':
			if err := flush(); err != nil {
				return nil, err
			}
		default:
			current.WriteByte(c)
			if c != ' ' && c != '\t' {
				elementStarted = true
			}
		}
	}
	if quoted {
		return nil, errors.New("unterminated quoted element")
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return out, nil
}
