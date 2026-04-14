package reaper

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/gocraft/dbr/v2/dialect"
	_ "github.com/jackc/pgx/v5/stdlib"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
	"github.com/Crawbl-AI/crawbl-backend/internal/userswarm/reaper/reaperrepo"
)

// Run is the main entry point for the reaper. It connects to Postgres and the
// Kubernetes API, finds all resources that need to be cleaned up, and deletes
// them. It returns a Result summarising what was done.
//
// Run is designed to be called once per CronJob invocation. It is not a
// long-running controller and does not watch for changes — it does a single
// snapshot query and acts on it.
//
// The cleanup happens in three phases:
//
//  1. Stale-CR phase: list every UserSwarm CR in the cluster, keep the ones
//     whose Kubernetes CreationTimestamp is older than cfg.MaxAge, and for
//     each of those look up the owning user by Spec.UserID. If the user is
//     an e2e test user (subject starts with "e2e-"), delete the CR and
//     soft-delete the user row. Driving off the CR's own age — instead of
//     the database user's created_at — means swarms get cleaned up even when
//     the user row was already soft-deleted out of band, and catches CRs
//     that have been lingering independent of their DB record.
//
//  2. Orphan sweep: scan every UserSwarm CR in the cluster and delete any
//     whose owning user no longer exists in the database. This catches swarms
//     that were left behind by out-of-band user deletions or earlier reaper
//     failures.
//
// Errors in individual per-user or per-swarm operations are counted and
// logged but do not abort the run. This means a single bad record cannot
// block cleanup of the remaining resources.
func Run(ctx context.Context, cfg *Config) (*Result, error) {
	logger := slog.Default()
	result := &Result{}

	// Open a connection pool to Postgres. We cap connections to 2 because the
	// reaper runs as a short-lived job and does not need high concurrency. The
	// pool is closed with defer so it is released even if we return early on
	// an error.
	conn, err := dbr.Open("pgx", cfg.DatabaseDSN, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	defer func() { _ = conn.Close() }()
	conn.Dialect = dialect.PostgreSQL
	conn.SetMaxOpenConns(2)
	conn.SetMaxIdleConns(1)
	sess := conn.NewSession(nil)

	// Build a controller-runtime client scoped to the UserSwarm CRD scheme.
	// This client reads the kubeconfig from the standard locations
	// (KUBECONFIG env var, in-cluster service account, ~/.kube/config).
	k8sClient, err := buildK8sClient()
	if err != nil {
		return nil, fmt.Errorf("build k8s client: %w", err)
	}

	// Calculate the cutoff timestamp. Any UserSwarm CR whose Kubernetes
	// CreationTimestamp is older than this moment is considered stale and
	// eligible for reaping. CRs created after the cutoff are still within the
	// MaxAge window and are left alone (they may belong to a test run that is
	// still in progress).
	cutoff := time.Now().UTC().Add(-cfg.MaxAge)
	repo := reaperrepo.New()

	// Phase 1: list every UserSwarm CR in the cluster and drive cleanup off
	// its own CreationTimestamp. We used to iterate the users table instead,
	// which meant a CR could outlive its row indefinitely — once the user was
	// soft-deleted out of band, phase 1 stopped seeing it and phase 2's
	// user-existence check was the only thing keeping the CR in scope. By
	// looking at CR age directly we catch stale swarms regardless of DB state.
	var swarmList crawblv1alpha1.UserSwarmList
	if err := k8sClient.List(ctx, &swarmList); err != nil {
		return nil, fmt.Errorf("list userswarms: %w", err)
	}

	logger.Info("scanning userswarm CRs for staleness",
		"total", len(swarmList.Items),
		"cutoff", cutoff.Format(time.RFC3339),
		"max_age", cfg.MaxAge,
	)

	// Track which users have already been soft-deleted in this run so we
	// don't issue a redundant UPDATE when the same e2e user owns multiple
	// stale CRs (one per workspace).
	softDeletedUsers := make(map[string]struct{})

	for i := range swarmList.Items {
		swarm := &swarmList.Items[i]

		// Skip CRs that have not yet aged past the cutoff.
		if swarm.CreationTimestamp.After(cutoff) {
			continue
		}

		found, reaped, userReaped, err := processStaleSwarm(ctx, k8sClient, sess, repo, logger, swarm, softDeletedUsers, cfg.DryRun)
		result.UsersFound += found
		result.SwarmsReaped += reaped
		result.UsersReaped += userReaped
		if err != nil {
			result.Errors++
		}
	}

	// Phase 2: orphan sweep. Scan all UserSwarm CRs in the cluster and remove
	// any that are no longer backed by an active user. This catches cases where:
	//   - A user was deleted directly in the database without going through the
	//     normal reaper flow (e.g. a manual admin action).
	//   - A previous reaper run soft-deleted the user but failed to delete the
	//     corresponding swarm CR.
	orphaned, errs := reapOrphanedSwarms(ctx, k8sClient, sess, repo, logger, cfg.DryRun)
	result.SwarmsReaped += orphaned
	result.Errors += errs

	return result, nil
}

// processStaleSwarm handles a single stale UserSwarm CR in phase 1 of the
// reaper loop. It looks up the owning user, validates it is an e2e subject,
// deletes the CR when appropriate, and soft-deletes the user row once.
//
// Returns (usersFound, swarmsReaped, usersReaped, err). A non-nil err means
// the operation should count as one error in Result.Errors.
func processStaleSwarm(
	ctx context.Context,
	k8sClient client.Client,
	sess *dbr.Session,
	repo *reaperrepo.Repo,
	logger *slog.Logger,
	swarm *crawblv1alpha1.UserSwarm,
	softDeletedUsers map[string]struct{},
	dryRun bool,
) (usersFound, swarmsReaped, usersReaped int, err error) {
	age := time.Since(swarm.CreationTimestamp.Time).Truncate(time.Minute)
	userID := swarm.Spec.UserID

	// Look up the owning user (including soft-deleted rows). Orphans (no user
	// row at all) are left to phase 2's dedicated orphan sweep.
	userRow, lookupErr := repo.FindUserByID(ctx, sess, userID)
	if lookupErr != nil {
		logger.Error("failed to look up user for stale swarm",
			"swarm", swarm.Name,
			"user_id", userID,
			"error", lookupErr,
		)
		return 0, 0, 0, lookupErr
	}
	if userRow == nil {
		return 0, 0, 0, nil
	}

	// Phase 1 is scoped to e2e users only.
	if !strings.HasPrefix(userRow.Subject, "e2e-") {
		return 0, 0, 0, nil
	}

	usersFound = 1
	logger.Info("processing stale userswarm",
		"swarm", swarm.Name,
		"subject", userRow.Subject,
		"email", userRow.Email,
		"age", age,
		"dry_run", dryRun,
	)

	if dryRun {
		swarmsReaped = 1
		if _, seen := softDeletedUsers[userRow.ID]; !seen {
			softDeletedUsers[userRow.ID] = struct{}{}
			usersReaped = 1
		}
		return usersFound, swarmsReaped, usersReaped, nil
	}

	if deleteErr := k8sClient.Delete(ctx, swarm); deleteErr != nil {
		if client.IgnoreNotFound(deleteErr) != nil {
			logger.Error("failed to delete stale userswarm", "name", swarm.Name, "error", deleteErr)
			return usersFound, 0, 0, deleteErr
		}
	}
	logger.Info("deleted stale userswarm", "name", swarm.Name, "user", userRow.Subject)
	swarmsReaped = 1

	// Soft-delete the user row once per user even if multiple CRs map back to it.
	if _, seen := softDeletedUsers[userRow.ID]; seen {
		return usersFound, swarmsReaped, 0, nil
	}
	softDeletedUsers[userRow.ID] = struct{}{}
	if userRow.DeletedAt != nil {
		// Already soft-deleted out of band — count it but skip the UPDATE.
		return usersFound, swarmsReaped, 1, nil
	}
	if softDeleteErr := repo.SoftDeleteUser(ctx, sess, userRow.ID); softDeleteErr != nil {
		logger.Error("failed to soft-delete user", "subject", userRow.Subject, "error", softDeleteErr)
		return usersFound, swarmsReaped, 0, softDeleteErr
	}
	logger.Info("reaped user", "subject", userRow.Subject)
	return usersFound, swarmsReaped, 1, nil
}

// reapOrphanedSwarms is the second cleanup phase. It lists every UserSwarm CR
// in the cluster and checks whether the user referenced by Spec.UserID still
// exists and is active in the database. Any CR whose user is missing or
// soft-deleted is treated as an orphan and deleted.
//
// This function is intentionally separate from reapUserSwarms so that it can
// run even when there are no stale e2e users to process. It acts as a safety
// net for swarms that were not cleaned up during the per-user phase (e.g.
// because a previous reaper run crashed mid-way through).
//
// The dryRun flag mirrors the behaviour in Run(): when true, candidates are
// logged and counted but not actually deleted.
func reapOrphanedSwarms(ctx context.Context, k8sClient client.Client, sess *dbr.Session, repo *reaperrepo.Repo, logger *slog.Logger, dryRun bool) (reaped, errors int) {
	// List all UserSwarm CRs cluster-wide. On a busy cluster this could return
	// hundreds of items, but in practice the number of test swarms is small
	// compared to the total. If the List call itself fails we return immediately
	// because we cannot safely proceed without the full picture.
	var swarmList crawblv1alpha1.UserSwarmList
	if err := k8sClient.List(ctx, &swarmList); err != nil {
		logger.Error("failed to list userswarms", "error", err)
		return 0, 1
	}

	for i := range swarmList.Items {
		swarm := &swarmList.Items[i]
		userID := swarm.Spec.UserID

		// Skip swarms that have no UserID in their spec. This should not happen
		// in practice (the client always sets it), but we guard against it to
		// avoid accidentally deleting legitimately unowned swarms.
		if userID == "" {
			continue
		}

		// Check whether the user is still active. The deleted_at IS NULL
		// guard ensures we treat soft-deleted users the same as missing ones —
		// both cases leave the swarm without a live owner.
		count, err := repo.CountActiveByID(ctx, sess, userID)
		if err != nil {
			logger.Error("failed to check user existence", "user_id", userID, "swarm", swarm.Name, "error", err)
			errors++
			continue
		}

		if count > 0 {
			// The user is alive and active — this swarm is legitimate, skip it.
			continue
		}

		logger.Info("found orphaned userswarm",
			"name", swarm.Name,
			"user_id", userID,
			"created", swarm.CreationTimestamp.Format(time.RFC3339),
			"dry_run", dryRun,
		)

		if dryRun {
			reaped++
			continue
		}

		if err := k8sClient.Delete(ctx, swarm); err != nil {
			if client.IgnoreNotFound(err) == nil {
				// Already gone; not an error.
				continue
			}
			logger.Error("failed to delete orphaned userswarm", "name", swarm.Name, "error", err)
			errors++
			continue
		}

		logger.Info("deleted orphaned userswarm", "name", swarm.Name)
		reaped++
	}

	return reaped, errors
}

// buildK8sClient constructs a controller-runtime client that knows about the
// UserSwarm CRD. The client resolves its kubeconfig from the standard chain:
//  1. KUBECONFIG environment variable
//  2. In-cluster service account (when running as a Kubernetes Job/CronJob)
//  3. ~/.kube/config (for local development)
//
// Only the crawblv1alpha1 scheme and the core meta scheme are registered
// because the reaper only needs to read and delete UserSwarm resources. Adding
// unnecessary schemes would increase startup time and memory usage.
func buildK8sClient() (client.Client, error) {
	scheme := runtime.NewScheme()
	// Register the UserSwarm CRD types so the client can encode/decode them.
	utilruntime.Must(crawblv1alpha1.AddToScheme(scheme))
	// Register core meta types (ObjectMeta, TypeMeta) required by all K8s objects.
	utilruntime.Must(metav1.AddMetaToScheme(scheme))

	restConfig, err := config.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("get kubeconfig: %w", err)
	}

	return client.New(restConfig, client.Options{Scheme: scheme})
}
