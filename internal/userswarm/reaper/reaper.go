package reaper

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/gocraft/dbr/v2/dialect"
	_ "github.com/lib/pq"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	crawblv1alpha1 "github.com/Crawbl-AI/crawbl-backend/api/v1alpha1"
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
	conn, err := dbr.Open("postgres", cfg.DatabaseDSN, nil)
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

		age := time.Since(swarm.CreationTimestamp.Time).Truncate(time.Minute)
		userID := swarm.Spec.UserID

		// Look up the owning user (including soft-deleted rows) so we can
		// decide whether this is an e2e swarm worth reaping in phase 1, and
		// so we can log a meaningful subject. Orphans (no user row at all)
		// are left to phase 2, which is the dedicated orphan sweep.
		user, err := findUserByID(ctx, sess, userID)
		if err != nil {
			logger.Error("failed to look up user for stale swarm",
				"swarm", swarm.Name,
				"user_id", userID,
				"error", err,
			)
			result.Errors++
			continue
		}
		if user == nil {
			// No user row at all — leave it to phase 2's orphan sweep so the
			// log output cleanly separates "stale e2e" from "orphaned".
			continue
		}

		// Phase 1 is scoped to e2e users. Stale CRs for real users are a
		// different class of problem (possibly a bug in the teardown path)
		// and should not be silently deleted here.
		if !strings.HasPrefix(user.Subject, "e2e-") {
			continue
		}

		result.UsersFound++
		logger.Info("processing stale userswarm",
			"swarm", swarm.Name,
			"subject", user.Subject,
			"email", user.Email,
			"age", age,
			"dry_run", cfg.DryRun,
		)

		if cfg.DryRun {
			result.SwarmsReaped++
			if _, seen := softDeletedUsers[user.ID]; !seen {
				softDeletedUsers[user.ID] = struct{}{}
				result.UsersReaped++
			}
			continue
		}

		if err := k8sClient.Delete(ctx, swarm); err != nil {
			if client.IgnoreNotFound(err) != nil {
				logger.Error("failed to delete stale userswarm", "name", swarm.Name, "error", err)
				result.Errors++
				continue
			}
		}
		logger.Info("deleted stale userswarm", "name", swarm.Name, "user", user.Subject)
		result.SwarmsReaped++

		// Soft-delete the user row once, even if multiple CRs map back to
		// the same user. The deleted_at IS NULL guard inside softDeleteUser
		// is a second line of defence against double-writes.
		if _, seen := softDeletedUsers[user.ID]; seen {
			continue
		}
		softDeletedUsers[user.ID] = struct{}{}
		if user.DeletedAt != nil {
			// User was already soft-deleted out of band (e.g. by an earlier
			// reaper run that failed after the DB update but before the CR
			// delete). Count it as reaped for visibility, but skip the
			// UPDATE since there's nothing to do.
			result.UsersReaped++
			continue
		}
		if err := softDeleteUser(ctx, sess, *user); err != nil {
			logger.Error("failed to soft-delete user", "subject", user.Subject, "error", err)
			result.Errors++
			continue
		}
		result.UsersReaped++
		logger.Info("reaped user", "subject", user.Subject)
	}

	// Phase 2: orphan sweep. Scan all UserSwarm CRs in the cluster and remove
	// any that are no longer backed by an active user. This catches cases where:
	//   - A user was deleted directly in the database without going through the
	//     normal reaper flow (e.g. a manual admin action).
	//   - A previous reaper run soft-deleted the user but failed to delete the
	//     corresponding swarm CR.
	orphaned, errs := reapOrphanedSwarms(ctx, k8sClient, sess, logger, cfg.DryRun)
	result.SwarmsReaped += orphaned
	result.Errors += errs

	return result, nil
}

// findUserByID looks up a single user row by primary key, including
// soft-deleted users. It returns (nil, nil) when no row exists so the
// caller can distinguish "missing" from "error". Soft-deleted users are
// returned with DeletedAt populated so phase 1 can skip the redundant
// UPDATE when the row was already torn down by a previous reaper run.
func findUserByID(ctx context.Context, sess *dbr.Session, id string) (*staleUser, error) {
	if id == "" {
		return nil, nil
	}
	var users []staleUser
	_, err := sess.Select("id", "subject", "email", "created_at", "deleted_at").
		From("users").
		Where("id = ?", id).
		Limit(1).
		LoadContext(ctx, &users)
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, nil
	}
	return &users[0], nil
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
func reapOrphanedSwarms(ctx context.Context, k8sClient client.Client, sess *dbr.Session, logger *slog.Logger, dryRun bool) (reaped, errors int) {
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

		// Check whether the user is still active. We query for a COUNT rather
		// than a full row to keep the query lightweight. The deleted_at IS NULL
		// guard ensures we treat soft-deleted users the same as missing ones —
		// both cases leave the swarm without a live owner.
		var count int
		err := sess.Select("COUNT(*)").
			From("users").
			Where("id = ? AND deleted_at IS NULL", userID).
			LoadOneContext(ctx, &count)
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

// softDeleteUser marks a user as deleted by setting deleted_at (and updated_at)
// to the current UTC time. We use a soft delete rather than a hard DELETE so
// that referential integrity is preserved for audit logs and any other tables
// that hold a foreign key to this user.
//
// The WHERE clause includes "deleted_at IS NULL" as a safety guard: if the
// user was already soft-deleted by another process (e.g. a concurrent reaper
// run or an explicit admin action) the UPDATE is a no-op rather than
// overwriting the original deletion timestamp.
func softDeleteUser(ctx context.Context, sess *dbr.Session, user staleUser) error {
	now := time.Now().UTC()
	_, err := sess.Update("users").
		Set("deleted_at", now).
		Set("updated_at", now).
		Where("id = ? AND deleted_at IS NULL", user.ID).
		ExecContext(ctx)
	return err
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
