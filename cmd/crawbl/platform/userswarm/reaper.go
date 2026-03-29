package userswarm

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/userswarm/reaper"
)

func newReaperCommand() *cobra.Command {
	var (
		databaseDSN string
		maxAge      time.Duration
		dryRun      bool
	)

	cmd := &cobra.Command{
		Use:   "reaper",
		Short: "Clean up stale test users and orphaned agent pods",
		Long: `Two-phase cleanup job for the dev cluster:

Phase 1: Finds users whose subject starts with "e2e-" and whose created_at
is older than --max-age, deletes their UserSwarm CRs (triggering teardown
of all agent pods, PVCs, and Services) and soft-deletes the user record.

Phase 2: Scans ALL UserSwarm CRs cluster-wide and deletes any whose owning
user no longer exists or has been soft-deleted. This is a universal safety
net that catches orphans from any source, not just e2e tests.

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
				DatabaseDSN: databaseDSN,
				MaxAge:      maxAge,
				DryRun:      dryRun,
			}

			result, err := reaper.Run(cmd.Context(), cfg)
			if err != nil {
				return fmt.Errorf("reaper failed: %w", err)
			}

			fmt.Fprintf(os.Stdout, "\nReaper summary:\n")
			fmt.Fprintf(os.Stdout, "  Users found:    %d\n", result.UsersFound)
			fmt.Fprintf(os.Stdout, "  Users reaped:   %d\n", result.UsersReaped)
			fmt.Fprintf(os.Stdout, "  Swarms reaped:  %d\n", result.SwarmsReaped)
			fmt.Fprintf(os.Stdout, "  Errors:         %d\n", result.Errors)

			if cfg.DryRun {
				fmt.Fprintln(os.Stdout, "  (dry run — no changes made)")
			}

			if result.Errors > 0 {
				return fmt.Errorf("reaper completed with %d errors", result.Errors)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&databaseDSN, "database-dsn", os.Getenv("CRAWBL_DATABASE_DSN"), "Postgres DSN (or set CRAWBL_DATABASE_DSN)")
	cmd.Flags().DurationVar(&maxAge, "max-age", 2*time.Hour, "Delete e2e users older than this duration")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Log what would be deleted without making changes")

	return cmd
}
