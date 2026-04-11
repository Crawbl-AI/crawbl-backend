package argocd

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// ReplaceImageTag loads a YAML stream (one or more documents separated
// by ---) and replaces every `image: <base>:<old>` value whose base
// matches imageBase with `<base>:<newTag>`. Multi-document streams are
// preserved — every document is re-emitted in original order so
// Kubernetes manifests containing multiple resources round-trip intact.
//
// Returns the updated YAML as a string.
func ReplaceImageTag(yamlContent, imageBase, newTag string) (string, error) {
	dec := yaml.NewDecoder(strings.NewReader(yamlContent))
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	for {
		var root yaml.Node
		if err := dec.Decode(&root); err != nil {
			if err == io.EOF {
				break
			}
			return "", fmt.Errorf("argocd: parse yaml: %w", err)
		}
		updateImageNodes(&root, imageBase, newTag)
		if err := enc.Encode(&root); err != nil {
			return "", fmt.Errorf("argocd: marshal yaml: %w", err)
		}
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("argocd: close yaml encoder: %w", err)
	}
	return out.String(), nil
}

// updateImageNodes recursively walks the yaml.Node tree looking for
// mapping keys named "image" whose value starts with imageBase + ":"
// and replaces the tag portion with newTag.
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
	for _, c := range n.Content {
		updateImageNodes(c, imageBase, newTag)
	}
}
