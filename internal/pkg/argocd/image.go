package argocd

import (
	"fmt"
	"os"
	"strings"
)

// ReplaceImageTag replaces any tag after imageBase (e.g. "registry.../name:") with newTag.
// It handles references appearing both bare and inside quoted strings.
func ReplaceImageTag(content, imageBase, newTag string) string {
	var result strings.Builder
	remaining := content
	for {
		idx := strings.Index(remaining, imageBase)
		if idx == -1 {
			result.WriteString(remaining)
			break
		}
		result.WriteString(remaining[:idx+len(imageBase)])
		after := remaining[idx+len(imageBase):]
		// The tag ends at whitespace, a quote, or end of string.
		end := strings.IndexAny(after, " \t\n\r\"'")
		if end == -1 {
			result.WriteString(newTag)
			break
		}
		result.WriteString(newTag)
		remaining = after[end:]
	}
	return result.String()
}

// ReplaceInFile replaces all occurrences of old with replacement in the file at path.
func ReplaceInFile(path, old, replacement string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	updated := strings.ReplaceAll(string(data), old, replacement)
	if err := os.WriteFile(path, []byte(updated), fileMode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
