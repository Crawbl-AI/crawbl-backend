package cloudflare

import (
	"fmt"

	cf "github.com/pulumi/pulumi-cloudflare/sdk/v5/go/cloudflare"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// NewCloudflare provisions Cloudflare tunnel and DNS resources for dev environments.
// When cfg.ManageTunnel is false the function returns immediately with a nil result,
// making it safe to call unconditionally from the top-level program.
func NewCloudflare(ctx *pulumi.Context, name string, cfg Config, opts ...pulumi.ResourceOption) (*Cloudflare, error) {
	if !cfg.ManageTunnel {
		return nil, nil
	}

	result := &Cloudflare{}

	tunnel, err := createTunnel(ctx, name, cfg, opts...)
	if err != nil {
		return nil, fmt.Errorf("create tunnel: %w", err)
	}
	result.Tunnel = tunnel

	tunnelCfg, err := createTunnelConfig(ctx, name, cfg, tunnel, opts...)
	if err != nil {
		return nil, fmt.Errorf("create tunnel config: %w", err)
	}
	result.TunnelConfig = tunnelCfg

	records, err := createDNSRecords(ctx, name, cfg, tunnel, opts...)
	if err != nil {
		return nil, fmt.Errorf("create dns records: %w", err)
	}
	result.Records = records

	setOutputs(result)
	return result, nil
}

// createTunnel provisions the Cloudflare Argo Tunnel.
func createTunnel(ctx *pulumi.Context, name string, cfg Config, opts ...pulumi.ResourceOption) (*cf.Tunnel, error) {
	tunnel, err := cf.NewTunnel(ctx, name+"-tunnel", &cf.TunnelArgs{
		AccountId: pulumi.String(cfg.AccountID),
		Name:      pulumi.String(cfg.TunnelName),
		Secret:    pulumi.String(cfg.TunnelSecret),
		// "cloudflare" config source means the tunnel ingress is managed via the
		// TunnelConfig resource below, not a local cloudflared YAML file.
		ConfigSrc: pulumi.StringPtr("cloudflare"),
	}, opts...)
	if err != nil {
		return nil, fmt.Errorf("new tunnel: %w", err)
	}
	return tunnel, nil
}

// createTunnelConfig sets the ingress routing rules for the tunnel.
// All configured subdomains route to the Envoy Gateway ClusterIP service with
// TLS verification disabled (internal cluster traffic, self-signed cert).
// A catch-all rule with no hostname is required by Cloudflare and returns 404.
func createTunnelConfig(ctx *pulumi.Context, name string, cfg Config, tunnel *cf.Tunnel, opts ...pulumi.ResourceOption) (*cf.TunnelConfig, error) {
	ingressRules := make(cf.TunnelConfigConfigIngressRuleArray, 0, len(cfg.Subdomains)+1)

	for _, sub := range cfg.Subdomains {
		hostname := sub + "." + cfg.ZoneName
		ingressRules = append(ingressRules, &cf.TunnelConfigConfigIngressRuleArgs{
			Hostname: pulumi.String(hostname),
			Service:  pulumi.String(cfg.EnvoyService),
			OriginRequest: &cf.TunnelConfigConfigIngressRuleOriginRequestArgs{
				NoTlsVerify: pulumi.Bool(true),
			},
		})
	}

	// Catch-all rule — required as the last entry, returns 404 for unmatched hosts.
	ingressRules = append(ingressRules, &cf.TunnelConfigConfigIngressRuleArgs{
		Service: pulumi.String("http_status:404"),
	})

	tunnelCfg, err := cf.NewTunnelConfig(ctx, name+"-tunnel-config", &cf.TunnelConfigArgs{
		AccountId: pulumi.String(cfg.AccountID),
		TunnelId:  tunnel.ID().ToStringOutput(),
		Config: &cf.TunnelConfigConfigArgs{
			IngressRules: ingressRules,
		},
	}, append(opts, pulumi.DependsOn([]pulumi.Resource{tunnel}))...)
	if err != nil {
		return nil, fmt.Errorf("new tunnel config: %w", err)
	}
	return tunnelCfg, nil
}

// createDNSRecords creates a proxied CNAME record for each subdomain pointing
// to <tunnel-id>.cfargotunnel.com. Proxied=true routes traffic through
// Cloudflare's edge network.
func createDNSRecords(ctx *pulumi.Context, name string, cfg Config, tunnel *cf.Tunnel, opts ...pulumi.ResourceOption) ([]*cf.Record, error) {
	records := make([]*cf.Record, 0, len(cfg.Subdomains))
	deps := append([]pulumi.ResourceOption{pulumi.DependsOn([]pulumi.Resource{tunnel})}, opts...)

	for _, sub := range cfg.Subdomains {
		hostname := sub + "." + cfg.ZoneName
		record, err := cf.NewRecord(ctx, name+"-dns-"+sub, &cf.RecordArgs{
			ZoneId:  pulumi.String(cfg.ZoneID),
			Name:    pulumi.String(hostname),
			Type:    pulumi.String("CNAME"),
			Value:   tunnel.Cname,
			Proxied: pulumi.Bool(true),
			Ttl:     pulumi.Int(1), // 1 = automatic TTL when proxied
		}, deps...)
		if err != nil {
			return nil, fmt.Errorf("new record for %s: %w", hostname, err)
		}
		records = append(records, record)
	}

	return records, nil
}

// setOutputs populates the Cloudflare.Outputs struct from resource attributes.
func setOutputs(c *Cloudflare) {
	c.Outputs.TunnelID = c.Tunnel.ID().ToStringOutput()
	c.Outputs.TunnelCNAME = c.Tunnel.Cname
}
