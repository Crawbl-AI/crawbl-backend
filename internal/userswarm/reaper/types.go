// Package reaper implements a periodic cleanup job that removes stale test
// users and orphaned UserSwarm CRs. It runs as a Kubernetes CronJob with two
// phases: Phase 1 targets e2e test users (subject starts with "e2e-") older
// than MaxAge. Phase 2 is a universal safety net that removes ANY UserSwarm
// CR whose owning user no longer exists — regardless of how that user was
// created or deleted.
//
// The earlier DigitalOcean block-volume sweep was removed in US-P2-011
// along with the rest of the PVC workflow. Runtime pods are stateless
// Deployments now and leave no external storage artifacts behind.
//
// # What the reaper cleans up
//
// The reaper targets two categories of resources:
//
//  1. Stale e2e test users — database rows in the "users" table whose subject
//     field starts with "e2e-" and whose created_at timestamp is older than the
//     configured MaxAge. For each such user the reaper also deletes the
//     UserSwarm CRs that belong to their workspaces.
//
//  2. Orphaned UserSwarm CRs — cluster-scoped Kubernetes custom resources that
//     reference a user ID that no longer exists (or has been soft-deleted) in
//     the database. This catches any swarms that were created but whose
//     corresponding user was removed out-of-band.
//
// # How it fits into the system
//
// UserSwarm CRs are created by the orchestrator when a user signs up
// (internal/userswarm/client). Metacontroller watches those CRs and reconciles
// the actual agent runtime pods and services in the "userswarms" namespace.
// The webhook (internal/userswarm/webhook) is notified by Metacontroller on
// every sync event and drives the desired-state diff. The reaper sits outside
// this reconciliation loop — it is a one-shot job (or cron job) that deletes
// CRs entirely, which causes Metacontroller to tear down the runtime.
//
// # Schedule / trigger
//
// The reaper is invoked as a Kubernetes CronJob (see crawbl-argocd-apps). It
// is not a long-running controller. Run() is called once per invocation, does
// its work, and exits. A typical schedule runs every few hours so that test
// infrastructure does not accumulate overnight.
//
// # Dry-run mode
//
// When Config.DryRun is true the reaper logs what it would delete but makes no
// changes to the database or the cluster. This is useful for validating the
// cutoff logic before a first production deployment.
package reaper

import (
	"log/slog"
	"time"

	"github.com/gocraft/dbr/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
	"github.com/Crawbl-AI/crawbl-backend/internal/userswarm/reaper/reaperrepo"
)

// Config holds the parameters that control a single reaper run.
//
// Callers construct a Config and pass it to Run(). All fields must be set;
// there are no defaults applied inside the package.
type Config struct {
	// DatabaseDSN is the libpq-style connection string for the Postgres database
	// that stores user and workspace records. Example:
	//   "postgres://user:pass@host:5432/crawbl?sslmode=require"
	DatabaseDSN string

	// MaxAge is the minimum age a test user must have before the reaper will
	// consider it stale. Users created more recently than MaxAge are left alone.
	// This prevents the reaper from accidentally deleting users from tests that
	// are still running. A value of 2–4 hours is typical for CI environments.
	MaxAge time.Duration

	// DryRun, when true, causes the reaper to log all candidates it would delete
	// but skip every mutating operation (database UPDATE and Kubernetes DELETE).
	// Result counters are still populated so the caller can see what would happen.
	DryRun bool
}

// Result summarises what happened during a reaper run. It is returned by Run()
// so that the caller (typically a CLI entrypoint or CronJob wrapper) can emit
// a structured log line or set a Prometheus gauge.
type Result struct {
	// UsersFound is the number of stale e2e users discovered in the database
	// before any deletion takes place.
	UsersFound int

	// UsersReaped is the number of users that were successfully soft-deleted
	// (or would have been deleted in dry-run mode).
	UsersReaped int

	// SwarmsReaped is the total number of UserSwarm CRs that were deleted from
	// the cluster, counting both user-owned swarms and orphaned swarms.
	SwarmsReaped int

	// Errors is the count of non-fatal errors encountered during the run.
	// The reaper continues processing remaining users/swarms even when
	// individual operations fail, so a non-zero Errors value does not mean
	// the entire run failed — just that some resources may need manual cleanup.
	Errors int
}

// processStaleSwarmOpts groups the inputs for processStaleSwarm.
type processStaleSwarmOpts struct {
	k8sClient        client.Client
	sess             *dbr.Session
	repo             *reaperrepo.Repo
	logger           *slog.Logger
	swarm            *crawblv1alpha1.UserSwarm
	softDeletedUsers map[string]struct{}
	dryRun           bool
}
