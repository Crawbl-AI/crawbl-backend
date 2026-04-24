package runtime

import (
	"fmt"
	"strings"
)

// buildCloudInit returns a cloud-init YAML string that installs k3s on first boot
// and configures the DOCR registry mirror.
func buildCloudInit(cfg RuntimeConfig) string {
	disableFlags := buildDisableFlags(cfg.K3s.Disable)

	return fmt.Sprintf(`#cloud-config
package_update: true
packages:
  - curl

runcmd:
  # Install k3s with configured version and disabled components.
  - curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION="%s" sh -s - server %s --write-kubeconfig-mode=%s

  # Create DOCR registry mirror configuration so k3s can pull images
  # from registry.digitalocean.com without additional auth setup.
  - mkdir -p /etc/rancher/k3s
  - |
    cat > /etc/rancher/k3s/registries.yaml <<'REGISTRIES'
    mirrors:
      registry.digitalocean.com:
        endpoint:
          - "https://registry.digitalocean.com"
    REGISTRIES

  # Wait for k3s to be ready before cloud-init reports success.
  - until kubectl get nodes --kubeconfig /etc/rancher/k3s/k3s.yaml 2>/dev/null; do sleep 5; done
`, cfg.K3s.Version, disableFlags, cfg.K3s.WriteKubeconfigMode)
}

// buildDisableFlags converts a list of component names into k3s --disable flags.
func buildDisableFlags(components []string) string {
	if len(components) == 0 {
		return ""
	}
	flags := make([]string, 0, len(components))
	for _, c := range components {
		flags = append(flags, "--disable="+c)
	}
	return strings.Join(flags, " ")
}
