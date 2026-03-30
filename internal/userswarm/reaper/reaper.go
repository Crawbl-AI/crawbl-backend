package reaper

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/gocraft/dbr/v2/dialect"
	_ "github.com/lib/pq"
	corev1 "k8s.io/api/core/v1"
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
// The cleanup happens in two phases:
//
//  1. Per-user phase: find all e2e test users older than cfg.MaxAge, delete
//     their UserSwarm CRs, then soft-delete the user row.
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
	defer conn.Close()
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

	// Calculate the cutoff timestamp. Any e2e user created before this moment
	// is considered stale and eligible for reaping. Users created after the
	// cutoff are still within the MaxAge window and are left alone (they may
	// belong to a test run that is still in progress).
	cutoff := time.Now().UTC().Add(-cfg.MaxAge)
	users, err := findStaleE2EUsers(ctx, sess, cutoff)
	if err != nil {
		return nil, fmt.Errorf("find stale e2e users: %w", err)
	}
	result.UsersFound = len(users)

	if len(users) == 0 {
		logger.Info("no stale e2e users found")
		return result, nil
	}

	logger.Info("found stale e2e users", "count", len(users), "cutoff", cutoff.Format(time.RFC3339))

	// Phase 1: process each stale user in turn.
	for _, user := range users {
		// Log the age of this record so it is easy to spot unusually old
		// test users in the output (e.g. from a hung CI run days ago).
		age := time.Since(user.CreatedAt).Truncate(time.Minute)
		logger.Info("processing stale user",
			"subject", user.Subject,
			"email", user.Email,
			"age", age,
			"dry_run", cfg.DryRun,
		)

		// In dry-run mode we count the user as "reaped" for reporting purposes
		// but skip all mutating operations.
		if cfg.DryRun {
			result.UsersReaped++
			continue
		}

		// Delete every UserSwarm CR that belongs to this user's workspaces.
		// This is done before soft-deleting the user so that the cluster
		// resources are removed first. If the swarm deletion fails for some
		// workspace, reapUserSwarms increments the error count and continues;
		// we still attempt to soft-delete the user so the database stays
		// consistent with a best-effort K8s cleanup.
		reaped, errs := reapUserSwarms(ctx, k8sClient, sess, logger, user)
		result.SwarmsReaped += reaped
		result.Errors += errs

		// Soft-delete the user by setting deleted_at. We use a soft delete
		// rather than a hard DELETE so that audit logs and foreign key
		// references in other tables remain intact. The WHERE clause guards
		// against racing with another reaper invocation or an explicit delete
		// triggered by the orchestrator.
		if err := softDeleteUser(ctx, sess, user); err != nil {
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

	// Phase 3: sweep DigitalOcean block volumes that no longer have a backing
	// PV. This catches leaked CSI volumes after PVC/PV teardown drift or after
	// a cluster was recreated and the old k8s-tagged volumes were left behind.
	volumesReaped, volumeErrs := reapOrphanedVolumes(ctx, k8sClient, logger, cfg)
	result.VolumesReaped += volumesReaped
	result.Errors += volumeErrs

	return result, nil
}

// findStaleE2EUsers queries the database for test users that are old enough to
// be reaped. It returns only users that:
//   - have a subject starting with "e2e-" (the convention used by the test
//     harness when calling POST /v1/auth/sign-up in e2e tests)
//   - have not yet been soft-deleted (deleted_at IS NULL)
//   - were created before the given cutoff timestamp
//
// Results are ordered oldest-first so the logs are easy to read chronologically.
func findStaleE2EUsers(ctx context.Context, sess *dbr.Session, cutoff time.Time) ([]staleUser, error) {
	var users []staleUser
	_, err := sess.Select("id", "subject", "email", "created_at").
		From("users").
		Where("subject LIKE 'e2e-%'").
		Where("deleted_at IS NULL").
		Where("created_at < ?", cutoff).
		OrderAsc("created_at").
		LoadContext(ctx, &users)
	if err != nil {
		return nil, err
	}
	return users, nil
}

// reapUserSwarms finds all workspaces owned by the given user, then deletes
// the UserSwarm CR for each workspace. It returns the number of swarms
// successfully deleted and the number of errors encountered.
//
// UserSwarm CRs are cluster-scoped (not namespace-scoped) and are named
// "workspace-<workspaceID>" by convention (see internal/userswarm/client).
// Once a CR is deleted, Metacontroller's sync hook fires and the webhook
// (internal/userswarm/webhook) tears down the ZeroClaw runtime pods, PVCs,
// and Services in the "userswarms" namespace.
//
// Errors are non-fatal: if one workspace's swarm cannot be deleted the
// function continues to the next workspace. The caller accumulates the error
// count and decides whether to surface it.
func reapUserSwarms(ctx context.Context, k8sClient client.Client, sess *dbr.Session, logger *slog.Logger, user staleUser) (reaped, errors int) {
	// Load the IDs of all workspaces belonging to this user. Each workspace
	// maps 1:1 to a UserSwarm CR in the cluster.
	var workspaceIDs []string
	_, err := sess.Select("id").
		From("workspaces").
		Where("user_id = ?", user.ID).
		LoadContext(ctx, &workspaceIDs)
	if err != nil {
		logger.Error("failed to list workspaces", "user_id", user.ID, "error", err)
		return 0, 1
	}

	for _, wsID := range workspaceIDs {
		swarmName := "workspace-" + wsID

		// Attempt to fetch the CR before deleting it. If it is already gone
		// (NotFound) we treat that as success — the desired end state (no CR)
		// is already achieved.
		var swarm crawblv1alpha1.UserSwarm
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: swarmName}, &swarm); err != nil {
			if client.IgnoreNotFound(err) == nil {
				// CR does not exist; nothing to delete.
				continue
			}
			logger.Error("failed to get userswarm", "name", swarmName, "error", err)
			errors++
			continue
		}

		if err := k8sClient.Delete(ctx, &swarm); err != nil {
			// A NotFound error on Delete means another process beat us to it;
			// that is fine and we do not count it as an error.
			if client.IgnoreNotFound(err) == nil {
				continue
			}
			logger.Error("failed to delete userswarm", "name", swarmName, "error", err)
			errors++
			continue
		}

		logger.Info("deleted userswarm", "name", swarmName, "user", user.Subject)
		reaped++
	}

	return reaped, errors
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
	// Register core API objects so the reaper can list PersistentVolumes.
	utilruntime.Must(corev1.AddToScheme(scheme))

	restConfig, err := config.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("get kubeconfig: %w", err)
	}

	return client.New(restConfig, client.Options{Scheme: scheme})
}
