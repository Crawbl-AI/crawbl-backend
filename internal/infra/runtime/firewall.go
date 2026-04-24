package runtime

import (
	"fmt"

	"github.com/pulumi/pulumi-hcloud/sdk/go/hcloud"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// createFirewall provisions a Hetzner Cloud firewall with rules for SSH, K8s API,
// HTTP/HTTPS inbound, and essential outbound traffic.
func createFirewall(ctx *pulumi.Context, name string, cfg RuntimeConfig) (*hcloud.Firewall, error) {
	k8sAPICIDRs := cfg.Hetzner.K8sAPIAllowedCIDRs
	if len(k8sAPICIDRs) == 0 {
		k8sAPICIDRs = cfg.Hetzner.SSHAllowedCIDRs
	}

	inbound := hcloud.FirewallRuleArray{
		buildInboundRule("allow-ssh", "22", "tcp", cfg.Hetzner.SSHAllowedCIDRs),
		buildInboundRule("allow-k8s-api", "6443", "tcp", k8sAPICIDRs),
		buildInboundRule("allow-http", "80", "tcp", []string{"0.0.0.0/0", "::/0"}),
		buildInboundRule("allow-https", "443", "tcp", []string{"0.0.0.0/0", "::/0"}),
	}

	outbound := hcloud.FirewallRuleArray{
		buildOutboundRule("allow-https-out", "443", "tcp"),
		buildOutboundRule("allow-http-out", "80", "tcp"),
		buildOutboundRule("allow-dns-tcp-out", "53", "tcp"),
		buildOutboundRule("allow-dns-udp-out", "53", "udp"),
	}

	rules := append(inbound, outbound...)

	fw, err := hcloud.NewFirewall(ctx, name+"-firewall", &hcloud.FirewallArgs{
		Name: pulumi.String(name + "-firewall"),
		Rules: rules,
		Labels: pulumi.StringMap{
			"managed-by": pulumi.String("pulumi"),
			"project":    pulumi.String(TagCrawbl),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create firewall: %w", err)
	}

	return fw, nil
}

// buildInboundRule constructs a single inbound firewall rule.
func buildInboundRule(description, port, protocol string, cidrs []string) hcloud.FirewallRuleArgs {
	sourceIPs := make(pulumi.StringArray, 0, len(cidrs))
	for _, cidr := range cidrs {
		sourceIPs = append(sourceIPs, pulumi.String(cidr))
	}
	return hcloud.FirewallRuleArgs{
		Description: pulumi.StringPtr(description),
		Direction:   pulumi.String("in"),
		Port:        pulumi.StringPtr(port),
		Protocol:    pulumi.String(protocol),
		SourceIps:   sourceIPs,
	}
}

// buildOutboundRule constructs a single outbound firewall rule.
func buildOutboundRule(description, port, protocol string) hcloud.FirewallRuleArgs {
	return hcloud.FirewallRuleArgs{
		Description:    pulumi.StringPtr(description),
		Direction:      pulumi.String("out"),
		Port:           pulumi.StringPtr(port),
		Protocol:       pulumi.String(protocol),
		DestinationIps: pulumi.StringArray{pulumi.String("0.0.0.0/0"), pulumi.String("::/0")},
	}
}
