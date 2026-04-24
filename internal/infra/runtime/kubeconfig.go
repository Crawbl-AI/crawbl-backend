package runtime

import (
	"fmt"
	"os"

	"github.com/pulumi/pulumi-command/sdk/go/command/remote"
	"github.com/pulumi/pulumi-hcloud/sdk/go/hcloud"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// extractKubeconfig SSHs into the Hetzner server after cloud-init completes,
// waits for k3s to become ready, and extracts the real kubeconfig from
// /etc/rancher/k3s/k3s.yaml. The kubeconfig's server address is rewritten
// from 127.0.0.1 to the server's public IPv4 address so it can be used
// from outside the cluster.
//
// This uses the Pulumi Command provider (remote.Command) which establishes
// an SSH connection to the server. The command polls until the k3s kubeconfig
// file exists (cloud-init is async), then outputs the rewritten kubeconfig.
func extractKubeconfig(ctx *pulumi.Context, name string, server *hcloud.Server, privateKey string) (pulumi.StringOutput, error) {
	if privateKey == "" {
		return pulumi.StringOutput{}, fmt.Errorf("SSH private key is required for kubeconfig extraction")
	}

	conn := remote.ConnectionArgs{
		Host:       server.Ipv4Address,
		User:       pulumi.String("root"),
		PrivateKey: pulumi.String(privateKey),
	}

	// Wait for cloud-init to finish and k3s to write its kubeconfig,
	// then output the kubeconfig with the public IP substituted in.
	cmd, err := remote.NewCommand(ctx, name+"-kubeconfig", &remote.CommandArgs{
		Connection: conn,
		Create: pulumi.Sprintf(
			`until [ -f /etc/rancher/k3s/k3s.yaml ]; do echo "waiting for k3s..." >&2; sleep 5; done; `+
				`sed 's|127.0.0.1|%s|g' /etc/rancher/k3s/k3s.yaml`,
			server.Ipv4Address,
		),
	}, pulumi.DependsOn([]pulumi.Resource{server}), pulumi.Timeouts(&pulumi.CustomTimeouts{
		Create: "10m",
	}))
	if err != nil {
		return pulumi.StringOutput{}, fmt.Errorf("create kubeconfig command: %w", err)
	}

	return cmd.Stdout, nil
}

// loadSSHPrivateKey reads the SSH private key from the ARGOCD_SSH_KEY_PATH
// env var, falling back to standard locations (~/.ssh/id_ed25519, ~/.ssh/id_rsa).
func loadSSHPrivateKey() (string, error) {
	// Try explicit path from env var first.
	if keyPath := os.Getenv("SSH_PRIVATE_KEY_PATH"); keyPath != "" {
		data, err := os.ReadFile(expandHome(keyPath))
		if err != nil {
			return "", fmt.Errorf("read SSH key from SSH_PRIVATE_KEY_PATH=%s: %w", keyPath, err)
		}
		return string(data), nil
	}

	// Fall back to standard SSH key locations.
	paths := []string{
		expandHome("~/.ssh/id_ed25519"),
		expandHome("~/.ssh/id_rsa"),
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err == nil && len(data) > 0 {
			return string(data), nil
		}
	}

	return "", fmt.Errorf("no SSH private key found at %v — set SSH_PRIVATE_KEY_PATH", paths)
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if len(path) > 1 && path[0] == '~' && path[1] == '/' {
		if home, err := os.UserHomeDir(); err == nil {
			return home + path[1:]
		}
	}
	return path
}
