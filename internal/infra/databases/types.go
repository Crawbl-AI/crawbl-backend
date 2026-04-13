// Package databases provides Pulumi resources for DigitalOcean managed databases.
package databases

import (
	"github.com/pulumi/pulumi-digitalocean/sdk/v4/go/digitalocean"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Config holds managed database configuration for a single environment.
// Only prod stacks populate this; dev uses self-hosted PG and Redis.
type Config struct {
	// PostgreSQL cluster settings.
	Postgres PostgresConfig `yaml:"postgres"`
	// Valkey cluster settings.
	Valkey ValkeyConfig `yaml:"valkey"`
	// PgBouncer connection pool settings.
	PgBouncer PgBouncerConfig `yaml:"pgbouncer"`
}

// PostgresConfig holds settings for the managed PostgreSQL cluster.
type PostgresConfig struct {
	// Name is the cluster name in DigitalOcean (e.g. "crawbl-prod-pg").
	Name string `yaml:"name"`
	// Version is the major PG version (e.g. "16").
	Version string `yaml:"version"`
	// Size is the DO droplet slug (e.g. "db-s-1vcpu-1gb").
	Size string `yaml:"size"`
	// NodeCount is the number of primary + replica nodes (1 = no replica).
	NodeCount int `yaml:"nodeCount"`
	// DBName is the application database to create inside the cluster.
	DBName string `yaml:"dbName"`
	// Tags are applied to the cluster in DigitalOcean.
	Tags []string `yaml:"tags"`
}

// ValkeyConfig holds settings for the managed Valkey cluster.
type ValkeyConfig struct {
	// Name is the cluster name in DigitalOcean (e.g. "crawbl-prod-valkey").
	Name string `yaml:"name"`
	// Version is the major Valkey version (e.g. "8").
	Version string `yaml:"version"`
	// Size is the DO droplet slug (e.g. "db-s-1vcpu-1gb").
	Size string `yaml:"size"`
	// NodeCount is the number of nodes (1 = single node, no replica).
	NodeCount int `yaml:"nodeCount"`
	// EvictionPolicy controls key eviction when memory is full.
	// Valid values: noeviction, allkeysLru, allkeysRandom, volatileLru, volatileRandom, volatileTtl.
	EvictionPolicy string `yaml:"evictionPolicy"`
	// Tags are applied to the cluster in DigitalOcean.
	Tags []string `yaml:"tags"`
}

// PgBouncerConfig holds settings for the PgBouncer connection pool.
type PgBouncerConfig struct {
	// Name is the pool name in DigitalOcean (e.g. "crawbl-pool").
	Name string `yaml:"name"`
	// DBName is the database to pool connections to (must match PostgresConfig.DBName).
	DBName string `yaml:"dbName"`
	// Mode is the PgBouncer pooling mode: session, transaction, or statement.
	Mode string `yaml:"mode"`
	// Size is the maximum number of connections in the pool.
	Size int `yaml:"size"`
	// User is the database user for pool connections. When empty, the default admin user is used.
	User string `yaml:"user"`
}

// StackDatabasesConfig is the YAML-serializable databases config read from Pulumi stack config.
// This is the single source of truth — values live in Pulumi.<env>.yaml, not in Go code.
type StackDatabasesConfig struct {
	Postgres  PostgresConfig  `yaml:"postgres"`
	Valkey    ValkeyConfig    `yaml:"valkey"`
	PgBouncer PgBouncerConfig `yaml:"pgbouncer"`
}

// Outputs contains connection details exported from managed database resources.
type Outputs struct {
	// PostgreSQL private connection URI (accessible only within the DO account/region).
	PGPrivateURI pulumi.StringOutput
	// PostgreSQL public host.
	PGHost pulumi.StringOutput
	// PostgreSQL port.
	PGPort pulumi.IntOutput

	// PgBouncer private connection URI.
	PgBouncerPrivateURI pulumi.StringOutput
	// PgBouncer private host.
	PgBouncerPrivateHost pulumi.StringOutput
	// PgBouncer port.
	PgBouncerPort pulumi.IntOutput

	// Valkey private connection URI.
	ValkeyPrivateURI pulumi.StringOutput
	// Valkey private host.
	ValkeyPrivateHost pulumi.StringOutput
	// Valkey port.
	ValkeyPort pulumi.IntOutput
}

// Databases groups all managed database resources created by this package.
type Databases struct {
	// PG is the managed PostgreSQL cluster.
	PG *digitalocean.DatabaseCluster
	// Valkey is the managed Valkey cluster.
	Valkey *digitalocean.DatabaseCluster
	// Pool is the PgBouncer connection pool attached to the PG cluster.
	Pool *digitalocean.DatabaseConnectionPool
	// Outputs holds exported connection details.
	Outputs Outputs
}
