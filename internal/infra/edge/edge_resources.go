package edge

import (
	"fmt"

	"github.com/pulumi/pulumi-cloudflare/sdk/v6/go/cloudflare"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apiextensions"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// createCloudflareSecret creates the Cloudflare API token secret.
func createCloudflareSecret(ctx *pulumi.Context, name string, cfg Config, opts ...pulumi.ResourceOption) (*corev1.Secret, error) {
	return corev1.NewSecret(ctx, name+"-cloudflare-secret", &corev1.SecretArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(cfg.CloudflareSecretName),
			Namespace: pulumi.String(cfg.CertManagerNamespace),
		},
		Type: pulumi.String("Opaque"),
		StringData: pulumi.ToStringMap(map[string]string{
			cfg.CloudflareSecretKey: cfg.CloudflareAPIToken,
		}),
	}, append(opts, pulumi.Provider(cfg.Provider))...)
}

// createClusterIssuer creates a cert-manager ClusterIssuer.
func createClusterIssuer(ctx *pulumi.Context, name string, cfg Config, opts ...pulumi.ResourceOption) (*apiextensions.CustomResource, error) {
	return apiextensions.NewCustomResource(ctx, name+"-cluster-issuer", &apiextensions.CustomResourceArgs{
		ApiVersion: pulumi.String("cert-manager.io/v1"),
		Kind:       pulumi.String("ClusterIssuer"),
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String(cfg.ClusterIssuerName),
		},
		OtherFields: map[string]interface{}{
			"spec": map[string]interface{}{
				"acme": map[string]interface{}{
					"email":  cfg.ACMEMail,
					"server": cfg.ACMEServer,
					"privateKeySecretRef": map[string]interface{}{
						"name": cfg.ClusterIssuerName + "-account-key",
					},
					"solvers": []interface{}{
						map[string]interface{}{
							"dns01": map[string]interface{}{
								"cloudflare": map[string]interface{}{
									"apiTokenSecretRef": map[string]interface{}{
										"name": cfg.CloudflareSecretName,
										"key":  cfg.CloudflareSecretKey,
									},
								},
							},
						},
					},
				},
			},
		},
	}, append(opts, pulumi.Provider(cfg.Provider))...)
}

// createCertificate creates a cert-manager Certificate for the edge gateway.
func createCertificate(ctx *pulumi.Context, name string, cfg Config, dnsNames []string, opts ...pulumi.ResourceOption) (*apiextensions.CustomResource, error) {
	dnsNamesIface := make([]interface{}, len(dnsNames))
	for i, n := range dnsNames {
		dnsNamesIface[i] = n
	}

	return apiextensions.NewCustomResource(ctx, name+"-certificate", &apiextensions.CustomResourceArgs{
		ApiVersion: pulumi.String("cert-manager.io/v1"),
		Kind:       pulumi.String("Certificate"),
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(cfg.GatewayTLSSecretName),
			Namespace: pulumi.String(cfg.GatewayNamespace),
		},
		OtherFields: map[string]interface{}{
			"spec": map[string]interface{}{
				"secretName": cfg.GatewayTLSSecretName,
				"issuerRef": map[string]interface{}{
					"name": cfg.ClusterIssuerName,
					"kind": "ClusterIssuer",
				},
				"dnsNames": dnsNamesIface,
			},
		},
	}, append(opts, pulumi.Provider(cfg.Provider))...)
}

// createGateway creates a Gateway API Gateway.
func createGateway(ctx *pulumi.Context, name string, cfg Config, opts ...pulumi.ResourceOption) (*apiextensions.CustomResource, error) {
	return apiextensions.NewCustomResource(ctx, name+"-gateway", &apiextensions.CustomResourceArgs{
		ApiVersion: pulumi.String("gateway.networking.k8s.io/v1"),
		Kind:       pulumi.String("Gateway"),
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(cfg.GatewayName),
			Namespace: pulumi.String(cfg.GatewayNamespace),
		},
		OtherFields: map[string]interface{}{
			"spec": map[string]interface{}{
				"gatewayClassName": cfg.GatewayClassName,
				"listeners": []interface{}{
					map[string]interface{}{
						"name":     cfg.GatewayListenerName,
						"port":     443,
						"protocol": "HTTPS",
						"tls": map[string]interface{}{
							"mode": "Terminate",
							"certificateRefs": []interface{}{
								map[string]interface{}{
									"kind": "Secret",
									"name": cfg.GatewayTLSSecretName,
								},
							},
						},
						"allowedRoutes": map[string]interface{}{
							"namespaces": map[string]interface{}{
								"from": "All",
							},
						},
					},
				},
			},
		},
	}, append(opts, pulumi.Provider(cfg.Provider))...)
}

// createDNSRecords creates Cloudflare DNS A records for all DNS names.
// The gatewayIP is obtained from the envoy proxy LoadBalancer service.
func createDNSRecords(ctx *pulumi.Context, name string, cfg Config, dnsNames []string, gatewayIP pulumi.StringOutput, deps []pulumi.Resource) ([]*cloudflare.Record, error) {
	zoneName := cfg.CloudflareZoneName
	zone, err := cloudflare.LookupZone(ctx, &cloudflare.LookupZoneArgs{
		Filter: &cloudflare.GetZoneFilter{
			Name:  &zoneName,
			Match: "all",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("lookup cloudflare zone: %w", err)
	}

	var records []*cloudflare.Record
	for _, dnsName := range dnsNames {
		record, err := cloudflare.NewRecord(ctx, name+"-dns-"+dnsName, &cloudflare.RecordArgs{
			ZoneId:  pulumi.String(zone.Id),
			Name:    pulumi.String(dnsName),
			Content: gatewayIP,
			Type:    pulumi.String("A"),
			Ttl:     pulumi.Float64(float64(cfg.DNSRecordTTL)),
			Proxied: pulumi.Bool(cfg.DNSRecordProxied),
		}, pulumi.DependsOn(deps))
		if err != nil {
			return nil, fmt.Errorf("create dns record %s: %w", dnsName, err)
		}
		records = append(records, record)
	}

	return records, nil
}
