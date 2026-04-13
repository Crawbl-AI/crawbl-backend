// Package infra provides Pulumi-based infrastructure management for Crawbl.
package infra

import (
	"github.com/pulumi/pulumi/sdk/v3/go/auto"

	"github.com/Crawbl-AI/crawbl-backend/internal/infra/cloudflare"
	"github.com/Crawbl-AI/crawbl-backend/internal/infra/cluster"
	"github.com/Crawbl-AI/crawbl-backend/internal/infra/databases"
	"github.com/Crawbl-AI/crawbl-backend/internal/infra/platform"
)

// Config holds all infrastructure configuration.
type Config struct {
	Environment    string
	Region         string
	ESCEnvironment string // Pulumi ESC environment (e.g. "crawbl/dev")
	ExistingVPCID  string
	ClusterConfig  cluster.Config
	PlatformConfig platform.Config
	// DatabasesConfig holds managed database settings.
	// Only populated for prod; dev uses self-hosted PG and Redis.
	DatabasesConfig *databases.Config
	// CloudflareConfig holds Cloudflare tunnel and DNS settings.
	// When CloudflareConfig.ManageTunnel is false, no Cloudflare resources are created.
	CloudflareConfig cloudflare.Config
}

// Stack represents a Pulumi stack.
type Stack struct {
	stack  auto.Stack
	config Config
}

// PreviewResult contains preview summary information.
type PreviewResult struct {
	Adds    int
	Updates int
	Deletes int
	Same    int
}

// UpResult contains apply result information.
type UpResult struct {
	Outputs map[string]any
}
