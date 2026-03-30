package userswarm

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
	"github.com/Crawbl-AI/crawbl-backend/internal/userswarm/reaper"
)

func newReaperCommand() *cobra.Command {
	var (
		databaseDSN        string
		maxAge             time.Duration
		orphanVolumeMinAge time.Duration
		digitalOceanToken  string
		dryRun             bool
	)

	cmd := &cobra.Command{
		Use:   "reaper",
		Short: "Clean up stale test users, orphaned swarms, and leaked DO volumes",
		Long: `Two-phase cleanup job for the dev cluster:

Phase 1: Finds users whose subject starts with "e2e-" and whose created_at
is older than --max-age, deletes their UserSwarm CRs (triggering teardown
of all agent pods, PVCs, and Services) and soft-deletes the user record.

Phase 2: Scans ALL UserSwarm CRs cluster-wide and deletes any whose owning
user no longer exists or has been soft-deleted. This is a universal safety
net that catches orphans from any source, not just e2e tests.

Phase 3: Lists DigitalOcean block volumes created by the CSI driver and
deletes any unattached pvc-* volumes that no longer map to a live PV in the
current cluster or that still carry a k8s:<cluster-id> tag for a deleted
cluster.

Designed to run as a Kubernetes CronJob using the crawbl-platform image.`,
		Example: `  # Dry run — see what would be cleaned up
  crawbl platform userswarm reaper --max-age 2h --dry-run

  # Reap stale e2e resources older than 2 hours
  crawbl platform userswarm reaper --max-age 2h

  # CronJob mode (reads DATABASE_DSN from env)
  crawbl platform userswarm reaper --max-age 2h`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if databaseDSN == "" {
				return fmt.Errorf("--database-dsn or CRAWBL_DATABASE_DSN is required")
			}

			cfg := &reaper.Config{
				DatabaseDSN:        databaseDSN,
				MaxAge:             maxAge,
				DryRun:             dryRun,
				DigitalOceanToken:  digitalOceanToken,
				OrphanVolumeMinAge: orphanVolumeMinAge,
			}

			result, err := reaper.Run(cmd.Context(), cfg)
			if err != nil {
				return fmt.Errorf("reaper failed: %w", err)
			}

			out.Ln()
			out.Step(style.Reaper, "Reaper summary")
			out.Infof("Users found:   %d", result.UsersFound)
			out.Infof("Users reaped:  %d", result.UsersReaped)
			out.Infof("Swarms reaped: %d", result.SwarmsReaped)
			out.Infof("Volumes reaped: %d", result.VolumesReaped)
			out.Infof("Errors:        %d", result.Errors)

			if cfg.DryRun {
				out.Step(style.Tip, "Dry run only — no changes were made")
			}

			if result.Errors > 0 {
				return fmt.Errorf("reaper completed with %d errors", result.Errors)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&databaseDSN, "database-dsn", os.Getenv("CRAWBL_DATABASE_DSN"), "Postgres DSN, or set CRAWBL_DATABASE_DSN")
	cmd.Flags().DurationVar(&maxAge, "max-age", 2*time.Hour, "Delete e2e users older than this duration")
	cmd.Flags().StringVar(&digitalOceanToken, "do-token", os.Getenv("DIGITALOCEAN_ACCESS_TOKEN"), "DigitalOcean API token, or set DIGITALOCEAN_ACCESS_TOKEN")
	cmd.Flags().DurationVar(&orphanVolumeMinAge, "orphan-volume-min-age", 30*time.Minute, "Only delete unattached DO volumes older than this duration")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Log what would be deleted without making changes")

	return cmd
}
