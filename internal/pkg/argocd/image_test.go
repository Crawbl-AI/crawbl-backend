package argocd

import (
	"strings"
	"testing"
)

func TestReplaceImageTagMultiDoc(t *testing.T) {
	// Mirror the real userswarm-webhook.yaml structure: Deployment then
	// Service separated by `---`. Only the Deployment has an image field.
	input := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: webhook
spec:
  template:
    spec:
      containers:
        - name: webhook
          image: registry.digitalocean.com/crawbl/crawbl-platform:old-tag
---
apiVersion: v1
kind: Service
metadata:
  name: webhook
spec:
  ports:
    - port: 443
      targetPort: 8443
  selector:
    app: webhook
`

	out, err := ReplaceImageTag(input, "registry.digitalocean.com/crawbl/crawbl-platform", "new-tag")
	if err != nil {
		t.Fatalf("ReplaceImageTag returned error: %v", err)
	}

	if !strings.Contains(out, "image: registry.digitalocean.com/crawbl/crawbl-platform:new-tag") {
		t.Errorf("expected new-tag in output, got:\n%s", out)
	}
	if strings.Contains(out, "image: registry.digitalocean.com/crawbl/crawbl-platform:old-tag") {
		t.Errorf("old-tag should be gone, got:\n%s", out)
	}
	if !strings.Contains(out, "kind: Service") {
		t.Errorf("second YAML document (Service) was dropped, got:\n%s", out)
	}
	if !strings.Contains(out, "targetPort: 8443") {
		t.Errorf("Service ports section was dropped, got:\n%s", out)
	}
	// Document separator must be preserved
	if strings.Count(out, "---") != 1 {
		t.Errorf("expected exactly one --- separator, got %d; output:\n%s", strings.Count(out, "---"), out)
	}
}

func TestReplaceImageTagSingleDoc(t *testing.T) {
	input := `apiVersion: v1
kind: ConfigMap
metadata:
  name: cfg
data:
  image: registry.digitalocean.com/crawbl/crawbl-platform:old
`
	out, err := ReplaceImageTag(input, "registry.digitalocean.com/crawbl/crawbl-platform", "new")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(out, ":new") {
		t.Errorf("new tag missing: %s", out)
	}
}

func TestReplaceImageTagNoMatch(t *testing.T) {
	input := "image: other/image:v1\n"
	out, err := ReplaceImageTag(input, "registry.digitalocean.com/crawbl/crawbl-platform", "new")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(out, "other/image:v1") {
		t.Errorf("unmatched image should be unchanged: %s", out)
	}
}
