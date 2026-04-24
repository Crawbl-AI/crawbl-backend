// Package runtime provides Pulumi resources for provisioning a Hetzner Cloud
// VM running k3s as a lightweight Kubernetes runtime for dev environments.
package runtime

import (
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/Crawbl-AI/crawbl-backend/internal/infra/cloudflare"
	"github.com/Crawbl-AI/crawbl-backend/internal/infra/platform"
)

const (
	// DefaultServerType is the default Hetzner server type for the k3s node.
	DefaultServerType = "cx43"
	// DefaultImage is the default OS image for the Hetzner server.
	DefaultImage = "ubuntu-24.04"
	// DefaultLocation is the default Hetzner datacenter location.
	DefaultLocation = "fsn1"
	// DefaultK3sVersion is the default k3s release version.
	DefaultK3sVersion = "v1.31.4+k3s1"
	// DefaultWriteKubeconfigMode is the default file permission mode for the k3s kubeconfig.
	DefaultWriteKubeconfigMode = "0640"

	// TagCrawbl marks every infrastructure resource managed by the crawbl Pulumi stack.
	TagCrawbl = "crawbl"
	// TagCrawblDev marks resources belonging to the dev environment.
	TagCrawblDev = "crawbl-dev"
	// TagK3s marks resources running k3s.
	TagK3s = "k3s"
)

// RuntimeConfig holds all configuration for provisioning a Hetzner k3s runtime.
type RuntimeConfig struct {
	Environment    string
	Region         string
	ESCEnvironment string
	Hetzner        HetznerConfig
	K3s            K3sConfig
	Platform       platform.Config
	Cloudflare     cloudflare.Config
}

// HetznerConfig holds Hetzner Cloud server settings.
type HetznerConfig struct {
	// ServerType is the Hetzner server plan (e.g. "cx43").
	ServerType string
	// Image is the OS image (e.g. "ubuntu-24.04").
	Image string
	// Location is the Hetzner datacenter (e.g. "fsn1").
	Location string
	// SSHAllowedCIDRs restricts SSH and K8s API access to these CIDR ranges.
	SSHAllowedCIDRs []string
	// SSHKeyFingerprints are the fingerprints of SSH keys to attach to the server.
	SSHKeyFingerprints []string
}

// K3sConfig holds k3s installation settings.
type K3sConfig struct {
	// Version is the k3s release version (e.g. "v1.31.4+k3s1").
	Version string
	// Disable is the list of k3s components to disable (e.g. "traefik", "servicelb").
	Disable []string
	// StorageClass is the default storage class name.
	StorageClass string
	// WriteKubeconfigMode is the file permission mode for /etc/rancher/k3s/k3s.yaml.
	WriteKubeconfigMode string
}

// RuntimeOutputs contains exported values from the runtime stack.
type RuntimeOutputs struct {
	// ServerIP is the public IPv4 address of the Hetzner server.
	ServerIP pulumi.StringOutput
	// Kubeconfig is the k3s kubeconfig with the server's public IP.
	Kubeconfig pulumi.StringOutput
	// ClusterName is the logical name of the k3s cluster.
	ClusterName pulumi.StringOutput
}

// RuntimeResult holds the outputs from a completed runtime provisioning.
type RuntimeResult struct {
	// Outputs contains the exported stack outputs.
	Outputs RuntimeOutputs
}

// Stack represents a Pulumi stack for the Hetzner k3s runtime.
type Stack struct {
	stack  auto.Stack
	config RuntimeConfig
}

// StackRuntimeConfig is the YAML-serializable runtime config read from
// Pulumi.<env>.yaml. This is the single source of truth -- values live in
// Pulumi.<env>.yaml, not in Go code.
type StackRuntimeConfig struct {
	Hetzner    StackHetznerConfig              `yaml:"hetzner"`
	K3s        StackK3sConfig                  `yaml:"k3s"`
	Cloudflare cloudflare.StackCloudflareConfig `yaml:"cloudflare"`
}

// StackHetznerConfig is the YAML form of Hetzner server settings.
type StackHetznerConfig struct {
	ServerType         string   `yaml:"serverType"`
	Image              string   `yaml:"image"`
	Location           string   `yaml:"location"`
	SSHAllowedCIDRs    []string `yaml:"sshAllowedCIDRs"`
	SSHKeyFingerprints []string `yaml:"sshKeyFingerprints"`
}

// StackK3sConfig is the YAML form of k3s installation settings.
type StackK3sConfig struct {
	Version             string   `yaml:"version"`
	Disable             []string `yaml:"disable"`
	StorageClass        string   `yaml:"storageClass"`
	WriteKubeconfigMode string   `yaml:"writeKubeconfigMode"`
}

// ConfigFromStack builds a RuntimeConfig from stack config and environment context.
func ConfigFromStack(env, region string, sc StackRuntimeConfig, platformCfg platform.Config) RuntimeConfig {
	hetzner := HetznerConfig{
		ServerType:         sc.Hetzner.ServerType,
		Image:              sc.Hetzner.Image,
		Location:           sc.Hetzner.Location,
		SSHAllowedCIDRs:    sc.Hetzner.SSHAllowedCIDRs,
		SSHKeyFingerprints: sc.Hetzner.SSHKeyFingerprints,
	}
	if hetzner.ServerType == "" {
		hetzner.ServerType = DefaultServerType
	}
	if hetzner.Image == "" {
		hetzner.Image = DefaultImage
	}
	if hetzner.Location == "" {
		hetzner.Location = DefaultLocation
	}
	if hetzner.SSHAllowedCIDRs == nil {
		hetzner.SSHAllowedCIDRs = []string{}
	}
	if hetzner.SSHKeyFingerprints == nil {
		hetzner.SSHKeyFingerprints = []string{}
	}

	k3s := K3sConfig{
		Version:             sc.K3s.Version,
		Disable:             sc.K3s.Disable,
		StorageClass:        sc.K3s.StorageClass,
		WriteKubeconfigMode: sc.K3s.WriteKubeconfigMode,
	}
	if k3s.Version == "" {
		k3s.Version = DefaultK3sVersion
	}
	if k3s.Disable == nil {
		k3s.Disable = []string{"traefik", "servicelb"}
	}
	if k3s.WriteKubeconfigMode == "" {
		k3s.WriteKubeconfigMode = DefaultWriteKubeconfigMode
	}

	cfCfg := cloudflare.Config{
		ManageTunnel: sc.Cloudflare.ManageTunnel,
		AccountID:    sc.Cloudflare.AccountID,
		ZoneID:       sc.Cloudflare.ZoneID,
		TunnelName:   sc.Cloudflare.TunnelName,
		TunnelID:     sc.Cloudflare.TunnelID,
		EnvoyService: sc.Cloudflare.EnvoyService,
		Subdomains:   sc.Cloudflare.Subdomains,
		ZoneName:     sc.Cloudflare.ZoneName,
	}

	return RuntimeConfig{
		Environment: env,
		Region:      region,
		Hetzner:     hetzner,
		K3s:         k3s,
		Platform:    platformCfg,
		Cloudflare:  cfCfg,
	}
}
