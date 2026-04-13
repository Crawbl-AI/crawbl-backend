package databases

import (
	"fmt"

	"github.com/pulumi/pulumi-digitalocean/sdk/v4/go/digitalocean"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// NewDatabases provisions managed PostgreSQL, Valkey, and PgBouncer resources
// for the prod environment. It returns a Databases struct populated with all
// created resources and their exported connection details.
//
// The clusterID argument is the DOKS cluster ID used to restrict database
// firewall access to nodes within the cluster.
func NewDatabases(ctx *pulumi.Context, name string, cfg Config, clusterID pulumi.StringInput, opts ...pulumi.ResourceOption) (*Databases, error) {
	result := &Databases{}

	pg, err := createPostgres(ctx, name, cfg, opts...)
	if err != nil {
		return nil, fmt.Errorf("create postgres: %w", err)
	}
	result.PG = pg

	valkey, err := createValkey(ctx, name, cfg, opts...)
	if err != nil {
		return nil, fmt.Errorf("create valkey: %w", err)
	}
	result.Valkey = valkey

	// Firewall rules restrict access to the DOKS cluster only.
	if err := createFirewalls(ctx, name, clusterID, pg, valkey, opts...); err != nil {
		return nil, fmt.Errorf("create firewalls: %w", err)
	}

	// crawbl application database inside the PG cluster.
	if err := createAppDatabase(ctx, name, cfg, pg, opts...); err != nil {
		return nil, fmt.Errorf("create app database: %w", err)
	}

	pool, err := createConnectionPool(ctx, name, cfg, pg, opts...)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}
	result.Pool = pool

	setOutputs(result)
	exportOutputs(ctx, result)

	return result, nil
}

// createPostgres creates the managed PostgreSQL cluster.
func createPostgres(ctx *pulumi.Context, name string, cfg Config, opts ...pulumi.ResourceOption) (*digitalocean.DatabaseCluster, error) {
	pg, err := digitalocean.NewDatabaseCluster(ctx, name+"-pg", &digitalocean.DatabaseClusterArgs{
		Name:      pulumi.String(cfg.Postgres.Name),
		Engine:    pulumi.String("pg"),
		Version:   pulumi.String(cfg.Postgres.Version),
		Size:      pulumi.String(cfg.Postgres.Size),
		Region:    pulumi.String("fra1"),
		NodeCount: pulumi.Int(cfg.Postgres.NodeCount),
		Tags:      pulumi.ToStringArray(cfg.Postgres.Tags),
	}, opts...)
	if err != nil {
		return nil, fmt.Errorf("new database cluster: %w", err)
	}
	return pg, nil
}

// createValkey creates the managed Valkey cluster.
func createValkey(ctx *pulumi.Context, name string, cfg Config, opts ...pulumi.ResourceOption) (*digitalocean.DatabaseCluster, error) {
	args := &digitalocean.DatabaseClusterArgs{
		Name:      pulumi.String(cfg.Valkey.Name),
		Engine:    pulumi.String("valkey"),
		Version:   pulumi.String(cfg.Valkey.Version),
		Size:      pulumi.String(cfg.Valkey.Size),
		Region:    pulumi.String("fra1"),
		NodeCount: pulumi.Int(cfg.Valkey.NodeCount),
		Tags:      pulumi.ToStringArray(cfg.Valkey.Tags),
	}
	if cfg.Valkey.EvictionPolicy != "" {
		args.EvictionPolicy = pulumi.String(cfg.Valkey.EvictionPolicy)
	}
	valkey, err := digitalocean.NewDatabaseCluster(ctx, name+"-valkey", args, opts...)
	if err != nil {
		return nil, fmt.Errorf("new database cluster: %w", err)
	}
	return valkey, nil
}

// createFirewalls creates DatabaseFirewall resources for PG and Valkey,
// restricting inbound connections to the DOKS cluster only.
func createFirewalls(ctx *pulumi.Context, name string, clusterID pulumi.StringInput, pg, valkey *digitalocean.DatabaseCluster, opts ...pulumi.ResourceOption) error {
	rules := digitalocean.DatabaseFirewallRuleArray{
		&digitalocean.DatabaseFirewallRuleArgs{
			Type:  pulumi.String("k8s"),
			Value: clusterID,
		},
	}

	_, err := digitalocean.NewDatabaseFirewall(ctx, name+"-pg-fw", &digitalocean.DatabaseFirewallArgs{
		ClusterId: pg.ID(),
		Rules:     rules,
	}, append(opts, pulumi.DependsOn([]pulumi.Resource{pg}))...)
	if err != nil {
		return fmt.Errorf("postgres firewall: %w", err)
	}

	_, err = digitalocean.NewDatabaseFirewall(ctx, name+"-valkey-fw", &digitalocean.DatabaseFirewallArgs{
		ClusterId: valkey.ID(),
		Rules:     rules,
	}, append(opts, pulumi.DependsOn([]pulumi.Resource{valkey}))...)
	if err != nil {
		return fmt.Errorf("valkey firewall: %w", err)
	}

	return nil
}

// createAppDatabase creates the application database inside the PG cluster.
func createAppDatabase(ctx *pulumi.Context, name string, cfg Config, pg *digitalocean.DatabaseCluster, opts ...pulumi.ResourceOption) error {
	_, err := digitalocean.NewDatabaseDb(ctx, name+"-pg-db", &digitalocean.DatabaseDbArgs{
		ClusterId: pg.ID(),
		Name:      pulumi.String(cfg.Postgres.DBName),
	}, append(opts, pulumi.DependsOn([]pulumi.Resource{pg}))...)
	if err != nil {
		return fmt.Errorf("new database db: %w", err)
	}
	return nil
}

// createConnectionPool creates the PgBouncer connection pool on the PG cluster.
func createConnectionPool(ctx *pulumi.Context, name string, cfg Config, pg *digitalocean.DatabaseCluster, opts ...pulumi.ResourceOption) (*digitalocean.DatabaseConnectionPool, error) {
	args := &digitalocean.DatabaseConnectionPoolArgs{
		ClusterId: pg.ID(),
		Name:      pulumi.String(cfg.PgBouncer.Name),
		DbName:    pulumi.String(cfg.PgBouncer.DBName),
		Mode:      pulumi.String(cfg.PgBouncer.Mode),
		Size:      pulumi.Int(cfg.PgBouncer.Size),
	}
	if cfg.PgBouncer.User != "" {
		args.User = pulumi.String(cfg.PgBouncer.User)
	}
	pool, err := digitalocean.NewDatabaseConnectionPool(ctx, name+"-pg-pool", args, append(opts, pulumi.DependsOn([]pulumi.Resource{pg}))...)
	if err != nil {
		return nil, fmt.Errorf("new database connection pool: %w", err)
	}
	return pool, nil
}

// setOutputs populates the Databases.Outputs struct from resource attributes.
func setOutputs(d *Databases) {
	d.Outputs.PGPrivateURI = d.PG.PrivateUri
	d.Outputs.PGHost = d.PG.PrivateHost
	d.Outputs.PGPort = d.PG.Port

	d.Outputs.PgBouncerPrivateURI = d.Pool.PrivateUri
	d.Outputs.PgBouncerPrivateHost = d.Pool.PrivateHost
	d.Outputs.PgBouncerPort = d.Pool.Port

	d.Outputs.ValkeyPrivateURI = d.Valkey.PrivateUri
	d.Outputs.ValkeyPrivateHost = d.Valkey.PrivateHost
	d.Outputs.ValkeyPort = d.Valkey.Port
}

// exportOutputs exports connection details as Pulumi stack outputs.
func exportOutputs(ctx *pulumi.Context, d *Databases) {
	ctx.Export("pgPrivateURI", d.Outputs.PGPrivateURI)
	ctx.Export("pgHost", d.Outputs.PGHost)
	ctx.Export("pgPort", d.Outputs.PGPort)
	ctx.Export("pgbouncerPrivateURI", d.Outputs.PgBouncerPrivateURI)
	ctx.Export("pgbouncerPrivateHost", d.Outputs.PgBouncerPrivateHost)
	ctx.Export("pgbouncerPort", d.Outputs.PgBouncerPort)
	ctx.Export("valkeyPrivateURI", d.Outputs.ValkeyPrivateURI)
	ctx.Export("valkeyPrivateHost", d.Outputs.ValkeyPrivateHost)
	ctx.Export("valkeyPort", d.Outputs.ValkeyPort)
}
