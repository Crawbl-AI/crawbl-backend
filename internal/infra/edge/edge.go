// Package edge provides Pulumi resources for edge infrastructure.
// It manages DNS records, Gateway API resources, and TLS certificates.
package edge

import (
	"fmt"
	"strings"

	"github.com/pulumi/pulumi-cloudflare/sdk/v6/go/cloudflare"
	"github.com/pulumi/pulumi-command/sdk/go/command/local"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apiextensions"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Config holds edge configuration.
type Config struct {
	Provider                *kubernetes.Provider
	CloudflareAPIToken      string
	CloudflareZoneName      string
	DNSRecordName           string
	AdditionalDNSRecords    []string
	DNSRecordTTL            int
	DNSRecordProxied        bool
	GatewayName             string
	GatewayNamespace        string
	GatewayClassName        string
	GatewayListenerName     string
	GatewayListenerHostname string
	ClusterIssuerName       string
	ACMEMail                string
	ACMEServer              string
	GatewayTLSSecretName    string
	CertManagerNamespace    string
	CloudflareSecretName    string
	CloudflareSecretKey     string
}

// DefaultEdgeConfig returns default edge configuration.
func DefaultEdgeConfig() Config {
	return Config{
		GatewayName:          "public-edge",
		GatewayNamespace:     "envoy-gateway-system",
		GatewayClassName:     "envoy-gateway-class",
		GatewayListenerName:  "https",
		ClusterIssuerName:    "letsencrypt-cloudflare",
		ACMEServer:           "https://acme-v02.api.letsencrypt.org/directory",
		GatewayTLSSecretName: "public-edge-tls",
		DNSRecordTTL:         120,
		DNSRecordProxied:     false,
		CertManagerNamespace: "cert-manager",
		CloudflareSecretName: "cloudflare-api-token",
		CloudflareSecretKey:  "api-token",
	}
}

// Edge represents edge infrastructure.
type Edge struct {
	DNSRecords    []*cloudflare.Record
	Gateway       *apiextensions.CustomResource
	ClusterIssuer *apiextensions.CustomResource
	Certificate   *apiextensions.CustomResource
	Secret        *corev1.Secret
}

// NewEdge creates edge infrastructure for DNS, Gateway, and TLS.
func NewEdge(ctx *pulumi.Context, name string, cfg Config, opts ...pulumi.ResourceOption) (*Edge, error) {
	result := &Edge{}

	// 1. Create Cloudflare API token secret for cert-manager
	secret, err := createCloudflareSecret(ctx, name, cfg, opts...)
	if err != nil {
		return nil, err
	}
	result.Secret = secret

	// 2. Create ClusterIssuer for Let's Encrypt
	issuer, err := createClusterIssuer(ctx, name, cfg, pulumi.DependsOn([]pulumi.Resource{secret}))
	if err != nil {
		return nil, err
	}
	result.ClusterIssuer = issuer

	// 3. Create Certificate
	dnsNames := allDNSNames(cfg)
	cert, err := createCertificate(ctx, name, cfg, dnsNames, pulumi.DependsOn([]pulumi.Resource{issuer}))
	if err != nil {
		return nil, err
	}
	result.Certificate = cert

	// 4. Create Gateway (depends on certificate so TLS secret exists)
	gateway, err := createGateway(ctx, name, cfg, pulumi.DependsOn([]pulumi.Resource{cert}))
	if err != nil {
		return nil, err
	}
	result.Gateway = gateway

	// 5. Create DNS records using the gateway's LoadBalancer IP
	if cfg.CloudflareZoneName != "" && len(dnsNames) > 0 {
		// Read the envoy proxy service's external IP via kubectl label selector.
		// The service is auto-created by the Envoy Gateway controller.
		cmd, err := local.NewCommand(ctx, name+"-gateway-ip", &local.CommandArgs{
			Create: pulumi.Sprintf(
				"kubectl get svc -n %s -l gateway.envoyproxy.io/owning-gateway-name=%s -o jsonpath='{.items[0].status.loadBalancer.ingress[0].ip}'",
				cfg.GatewayNamespace, cfg.GatewayName,
			),
		}, pulumi.DependsOn([]pulumi.Resource{gateway}))
		if err != nil {
			return nil, fmt.Errorf("lookup gateway address: %w", err)
		}

		gatewayIP := cmd.Stdout.ApplyT(func(s string) string {
			// Strip surrounding quotes from jsonpath output
			return strings.Trim(s, "'\"")
		}).(pulumi.StringOutput)

		records, err := createDNSRecords(ctx, name, cfg, dnsNames, gatewayIP, []pulumi.Resource{gateway, cmd})
		if err != nil {
			return nil, err
		}
		result.DNSRecords = records
	}

	ctx.Export("gatewayName", pulumi.String(cfg.GatewayName))
	ctx.Export("gatewayNamespace", pulumi.String(cfg.GatewayNamespace))

	return result, nil
}

// allDNSNames returns the primary DNS name plus any additional ones.
func allDNSNames(cfg Config) []string {
	var names []string
	if cfg.DNSRecordName != "" {
		names = append(names, cfg.DNSRecordName)
	}
	names = append(names, cfg.AdditionalDNSRecords...)
	return names
}
