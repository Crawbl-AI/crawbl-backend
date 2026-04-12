package argocd

import (
	"fmt"
	"regexp"
)

// ReplaceImageTag replaces every occurrence of `<imageBase>:<oldTag>` in YAML
// lines that use `image:` or `value:` keys. Multi-document streams and all
// formatting are preserved because the replacement operates on individual
// lines rather than parsing and re-emitting the YAML structure.
func ReplaceImageTag(yamlContent, imageBase, newTag string) (string, error) {
	if imageBase == "" || newTag == "" {
		return "", fmt.Errorf("argocd: imageBase and newTag must not be empty")
	}
	// Match lines like:
	//   image: registry.digitalocean.com/crawbl/crawbl-platform:v1.2.3
	//   image: "registry.digitalocean.com/crawbl/crawbl-platform:v1.2.3"
	//   value: registry.digitalocean.com/crawbl/crawbl-agent-runtime:v0.6.1
	// Handles optional leading whitespace and optional quotes on both
	// `image:` and `value:` YAML keys.
	pattern := regexp.MustCompile(
		`(?m)(^\s*(?:image|value):\s*"?)` + regexp.QuoteMeta(imageBase) + `:[^\s"]*("?\s*$)`,
	)
	replaced := pattern.ReplaceAllString(yamlContent, "${1}"+imageBase+":"+newTag+"${2}")
	return replaced, nil
}
