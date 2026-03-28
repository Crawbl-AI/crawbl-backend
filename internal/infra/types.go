// Package infra provides Pulumi-based infrastructure management for Crawbl.
package infra

import (
	"github.com/pulumi/pulumi/sdk/v3/go/auto"

	"github.com/Crawbl-AI/crawbl-backend/internal/infra/cluster"
	"github.com/Crawbl-AI/crawbl-backend/internal/infra/platform"
)

// Config holds all infrastructure configuration.
type Config struct {
	Environment        string
	Region             string
	ESCEnvironment     string // Pulumi ESC environment (e.g. "crawbl/dev")
	ExistingVPCID  string
	ClusterConfig  cluster.Config
	PlatformConfig platform.Config
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
	Outputs map[string]interface{}
}
