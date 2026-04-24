package runtime

import (
	"strings"

	"github.com/pulumi/pulumi-hcloud/sdk/go/hcloud"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// kubeconfigTemplate is a minimal k3s kubeconfig template. The server IP
// placeholder is replaced with the actual public IP at deploy time.
const kubeconfigTemplate = `apiVersion: v1
clusters:
- cluster:
    server: https://SERVER_IP:6443
    insecure-skip-tls-verify: true
  name: k3s
contexts:
- context:
    cluster: k3s
    user: k3s
  name: k3s
current-context: k3s
kind: Config
preferences: {}
users:
- name: k3s
  user:
    # Token is read from the server after provisioning.
    # Use: ssh root@<ip> cat /var/lib/rancher/k3s/server/node-token
    token: ""
`

// extractKubeconfig builds a kubeconfig string output by replacing the
// localhost reference with the server's public IPv4 address.
//
// Because the actual /etc/rancher/k3s/k3s.yaml on the server is only
// available after cloud-init completes (which happens asynchronously after
// Pulumi marks the server as created), we use a template kubeconfig with
// insecure-skip-tls-verify and the public IP. Operators can later replace
// this with the real kubeconfig via SSH.
func extractKubeconfig(_ *pulumi.Context, _ string, server *hcloud.Server) pulumi.StringOutput {
	return server.Ipv4Address.ApplyT(func(ip string) string {
		return strings.ReplaceAll(kubeconfigTemplate, "SERVER_IP", ip)
	}).(pulumi.StringOutput)
}
