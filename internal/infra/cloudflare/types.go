// Package cloudflare provides Pulumi resources for Cloudflare tunnel and DNS management.
package cloudflare

import (
	cf "github.com/pulumi/pulumi-cloudflare/sdk/v5/go/cloudflare"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Config holds Cloudflare infrastructure configuration.
// ManageTunnel gates all resource creation — when false the entire package is a no-op.
type Config struct {
	// ManageTunnel enables Pulumi management of the CF tunnel and DNS records.
	// Set to true for dev; prod uses a DigitalOcean Load Balancer instead.
	ManageTunnel bool `yaml:"manageTunnel"`

	// AccountID is the Cloudflare account identifier.
	AccountID string `yaml:"accountID"`

	// ZoneID is the Cloudflare zone identifier for crawbl.com.
	ZoneID string `yaml:"zoneID"`

	// TunnelName is the name of the Cloudflare tunnel (e.g. "crawbl-dev").
	TunnelName string `yaml:"tunnelName"`

	// TunnelID is the ID of a pre-existing tunnel to import rather than create.
	// When non-empty, Pulumi imports this tunnel instead of provisioning a new one.
	TunnelID string `yaml:"tunnelID"`

	// TunnelSecret is the base64-encoded 32-byte tunnel secret.
	// Sourced from the CLOUDFLARE_TUNNEL_SECRET environment variable at deploy time.
	TunnelSecret string `yaml:"-"`

	// EnvoyService is the cluster-internal address that all tunnel ingress rules
	// route traffic to (e.g. "https://envoy-...svc.cluster.local:443").
	EnvoyService string `yaml:"envoyService"`

	// Subdomains is the list of hostnames (without the zone apex) that receive
	// CNAME records pointing at the tunnel (e.g. "api-dev", "river-dev").
	Subdomains []string `yaml:"subdomains"`

	// ZoneName is the DNS zone apex (e.g. "crawbl.com"). Combined with each
	// subdomain to form the full hostname for DNS records.
	ZoneName string `yaml:"zoneName"`
}

// StackCloudflareConfig is the YAML-serializable Cloudflare config read from
// Pulumi.<env>.yaml. This is the single source of truth — values live in
// Pulumi.<env>.yaml, not in Go code.
type StackCloudflareConfig struct {
	ManageTunnel bool     `yaml:"manageTunnel"`
	AccountID    string   `yaml:"accountID"`
	ZoneID       string   `yaml:"zoneID"`
	TunnelName   string   `yaml:"tunnelName"`
	TunnelID     string   `yaml:"tunnelID"`
	EnvoyService string   `yaml:"envoyService"`
	Subdomains   []string `yaml:"subdomains"`
	ZoneName     string   `yaml:"zoneName"`
}

// Outputs contains exported values from Cloudflare resources.
type Outputs struct {
	// TunnelID is the Cloudflare tunnel UUID.
	TunnelID pulumi.StringOutput
	// TunnelCNAME is the <tunnel-id>.cfargotunnel.com address DNS records point to.
	TunnelCNAME pulumi.StringOutput
}

// Cloudflare groups all Cloudflare resources created by this package.
type Cloudflare struct {
	// Tunnel is the Argo Tunnel resource.
	Tunnel *cf.Tunnel
	// TunnelConfig is the ingress routing configuration for the tunnel.
	TunnelConfig *cf.TunnelConfig
	// Records is the list of CNAME DNS records created for each subdomain.
	Records []*cf.Record
	// Outputs holds exported values.
	Outputs Outputs
}
