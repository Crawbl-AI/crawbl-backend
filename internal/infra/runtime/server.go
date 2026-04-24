package runtime

import (
	"fmt"

	"github.com/pulumi/pulumi-hcloud/sdk/go/hcloud"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// createServer provisions a Hetzner Cloud server with the given cloud-init
// user data and attaches the provided firewall.
func createServer(ctx *pulumi.Context, name string, cfg RuntimeConfig, firewall *hcloud.Firewall, cloudInit string) (*hcloud.Server, error) {
	sshKeys := make(pulumi.StringArray, 0, len(cfg.Hetzner.SSHKeyFingerprints))
	for _, fp := range cfg.Hetzner.SSHKeyFingerprints {
		sshKeys = append(sshKeys, pulumi.String(fp))
	}

	server, err := hcloud.NewServer(ctx, name+"-server", &hcloud.ServerArgs{
		Name:       pulumi.String(name),
		ServerType: pulumi.String(cfg.Hetzner.ServerType),
		Image:      pulumi.String(cfg.Hetzner.Image),
		Location:   pulumi.StringPtr(cfg.Hetzner.Location),
		UserData:   pulumi.StringPtr(cloudInit),
		SshKeys:    sshKeys,
		Labels: pulumi.StringMap{
			"managed-by":  pulumi.String("pulumi"),
			"project":     pulumi.String(TagCrawbl),
			"environment": pulumi.String(TagCrawblDev),
			"role":        pulumi.String(TagK3s),
		},
		FirewallIds: pulumi.IntArray{
			firewall.ID().ToIDOutput().ApplyT(func(id pulumi.ID) int {
				// Hetzner firewall IDs are integers; Pulumi returns them as ID strings.
				// The hcloud provider accepts int arrays for firewall attachment.
				var n int
				_, _ = fmt.Sscanf(string(id), "%d", &n)
				return n
			}).(pulumi.IntOutput),
		},
		PublicNets: hcloud.ServerPublicNetArray{
			&hcloud.ServerPublicNetArgs{
				Ipv4Enabled: pulumi.Bool(true),
				Ipv6Enabled: pulumi.Bool(true),
			},
		},
	}, pulumi.DependsOn([]pulumi.Resource{firewall}))
	if err != nil {
		return nil, fmt.Errorf("create server: %w", err)
	}

	return server, nil
}
