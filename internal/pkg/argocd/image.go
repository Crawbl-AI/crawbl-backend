package argocd

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ReplaceImageTag loads a YAML document and replaces every `image: <base>:...`
// value whose base matches imageBase with `<base>:newTag`. Returns the updated
// YAML as a string. Uses yaml.v3 structured editing rather than substring
// replacement so it handles multi-tag manifests and quoted values correctly.
func ReplaceImageTag(content, imageBase, newTag string) (string, error) {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		return "", fmt.Errorf("argocd: parse yaml: %w", err)
	}
	updateImageNodes(&root, imageBase, newTag)
	out, err := yaml.Marshal(&root)
	if err != nil {
		return "", fmt.Errorf("argocd: marshal yaml: %w", err)
	}
	return string(out), nil
}

// updateImageNodes recursively walks the yaml.Node tree looking for mapping
// keys named "image" whose value starts with imageBase + ":" and replaces
// the tag portion with newTag.
func updateImageNodes(n *yaml.Node, imageBase, newTag string) {
	if n == nil {
		return
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i]
			val := n.Content[i+1]
			if key.Value == "image" && val.Kind == yaml.ScalarNode {
				prefix := imageBase + ":"
				if strings.HasPrefix(val.Value, prefix) {
					val.Value = imageBase + ":" + newTag
				}
			}
			updateImageNodes(val, imageBase, newTag)
		}
		return
	}
	if n.Kind == yaml.SequenceNode || n.Kind == yaml.DocumentNode {
		for _, c := range n.Content {
			updateImageNodes(c, imageBase, newTag)
		}
	}
}
