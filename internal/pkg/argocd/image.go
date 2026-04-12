package argocd

import (
	"fmt"
	"regexp"
)

// ReplaceImageTag replaces every `image: <imageBase>:<oldTag>` line in the
// YAML content with `image: <imageBase>:<newTag>`. Multi-document streams
// and all formatting are preserved because the replacement operates on
// individual lines rather than parsing and re-emitting the YAML structure.
func ReplaceImageTag(yamlContent, imageBase, newTag string) (string, error) {
	if imageBase == "" || newTag == "" {
		return "", fmt.Errorf("argocd: imageBase and newTag must not be empty")
	}
	// Match lines like:
	//   image: registry.digitalocean.com/crawbl/platform:v1.2.3
	//   image: "registry.digitalocean.com/crawbl/platform:v1.2.3"
	// Handles optional leading whitespace and optional quotes.
	pattern := regexp.MustCompile(
		`(?m)(^\s*image:\s*"?)` + regexp.QuoteMeta(imageBase) + `:[^\s"]*("?\s*$)`,
	)
	replaced := pattern.ReplaceAllString(yamlContent, "${1}"+imageBase+":"+newTag+"${2}")
	return replaced, nil
}
