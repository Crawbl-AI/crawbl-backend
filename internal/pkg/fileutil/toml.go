package fileutil

import (
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
)

// DecodeTOML parses raw TOML bytes into a generic map.
// Useful when you need to manipulate TOML documents without typed structs
// (e.g. selective key merging where only some keys are known at compile time).
func DecodeTOML(raw []byte) (map[string]any, error) {
	doc := map[string]any{}
	if _, err := toml.Decode(string(raw), &doc); err != nil {
		return nil, fmt.Errorf("decode toml: %w", err)
	}
	return doc, nil
}

// ApplyTOMLOverrides decodes raw TOML text on top of an existing struct.
// Any keys present in the override text replace the corresponding fields in dst.
// Empty/whitespace-only overrides are a no-op.
//
// This is useful as an escape hatch: let users provide raw TOML that overrides
// fields not exposed in a CRD or config struct.
func ApplyTOMLOverrides(dst any, overrides string) error {
	if strings.TrimSpace(overrides) == "" {
		return nil
	}
	if _, err := toml.Decode(strings.TrimSpace(overrides), dst); err != nil {
		return fmt.Errorf("apply toml overrides: %w", err)
	}
	return nil
}
