package runtime

import (
	"fmt"
	"strings"
)

// buildCloudInit returns a cloud-init YAML string that installs k3s on first boot,
// hardens SSH, enables automatic security updates, configures fail2ban, and
// verifies the k3s binary checksum before installation.
func buildCloudInit(cfg RuntimeConfig) string {
	disableFlags := buildDisableFlags(cfg.K3s.Disable)

	return fmt.Sprintf(`#cloud-config
package_update: true
package_upgrade: true
packages:
  - curl
  - unattended-upgrades
  - fail2ban
  - jq

# Harden SSH — disable password auth, restrict root to key-only.
write_files:
  - path: /etc/ssh/sshd_config.d/99-hardening.conf
    content: |
      PasswordAuthentication no
      PermitRootLogin prohibit-password
      MaxAuthTries 3
      LoginGraceTime 20
    permissions: '0644'

  - path: /etc/fail2ban/jail.local
    content: |
      [sshd]
      enabled = true
      maxretry = 3
      bantime = 3600
      findtime = 600
    permissions: '0644'

  - path: /etc/rancher/k3s/registries.yaml
    content: |
      mirrors:
        registry.digitalocean.com:
          endpoint:
            - "https://registry.digitalocean.com"
    permissions: '0640'

runcmd:
  # Restart SSH with hardened config.
  - systemctl restart sshd

  # Enable fail2ban.
  - systemctl enable fail2ban
  - systemctl start fail2ban

  # Enable automatic security updates.
  - dpkg-reconfigure -plow unattended-upgrades

  # Install k3s with checksum verification.
  # Download the binary and its checksum separately, verify, then install.
  - curl -sfL -o /tmp/k3s https://github.com/k3s-io/k3s/releases/download/%s/k3s
  - curl -sfL -o /tmp/k3s-sha256.txt https://github.com/k3s-io/k3s/releases/download/%s/sha256sum-amd64.txt
  - |
    EXPECTED=$(grep ' k3s$' /tmp/k3s-sha256.txt | awk '{print $1}')
    ACTUAL=$(sha256sum /tmp/k3s | awk '{print $1}')
    if [ "$EXPECTED" != "$ACTUAL" ]; then
      echo "FATAL: k3s binary checksum mismatch" >&2
      exit 1
    fi
  - chmod +x /tmp/k3s
  - cp /tmp/k3s /usr/local/bin/k3s
  - curl -sfL https://get.k3s.io | INSTALL_K3S_SKIP_DOWNLOAD=true INSTALL_K3S_VERSION="%s" sh -s - server %s --write-kubeconfig-mode=%s

  # Wait for k3s to be ready before cloud-init reports success.
  - until kubectl get nodes --kubeconfig /etc/rancher/k3s/k3s.yaml 2>/dev/null; do sleep 5; done
`, cfg.K3s.Version, cfg.K3s.Version, cfg.K3s.Version, disableFlags, cfg.K3s.WriteKubeconfigMode)
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
