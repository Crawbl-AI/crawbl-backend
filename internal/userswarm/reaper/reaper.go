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
// reaperMaxOpenConns caps the DB pool used by the short-lived reaper job.
const reaperMaxOpenConns = 2

// reaperMaxIdleConns caps the idle pool for the reaper's short-lived job.
const reaperMaxIdleConns = 1

// e2eUserSubjectPrefix identifies e2e test users whose stale swarms phase 1
// is allowed to clean up.
const e2eUserSubjectPrefix = "e2e-"

func Run(ctx context.Context, cfg *Config) (*Result, error) {
	logger := slog.Default()
	result := &Result{}

	conn, err := openReaperDB(cfg.DatabaseDSN)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	sess := conn.NewSession(nil)

	// Build a controller-runtime client scoped to the UserSwarm CRD scheme.
	// This client reads the kubeconfig from the standard locations
	// (KUBECONFIG env var, in-cluster service account, ~/.kube/config).
	k8sClient, err := buildK8sClient()
	if err != nil {
		return nil, fmt.Errorf("build k8s client: %w", err)
	}

	// Cutoff: any CR whose CreationTimestamp is older than this moment is
	// stale. Younger CRs may belong to an in-progress test and are skipped.
	cutoff := time.Now().UTC().Add(-cfg.MaxAge)
	repo := reaperrepo.New()

	if err := runStaleSwarmPhase(ctx, k8sClient, sess, repo, logger, cfg, cutoff, result); err != nil {
		return nil, err
	}

	// Phase 2 orphan sweep: scan all UserSwarm CRs in the cluster and remove
	// any that are no longer backed by an active user. Catches out-of-band
	// user deletions and partial reaps from earlier runs.
	orphaned, errs := reapOrphanedSwarms(ctx, k8sClient, sess, repo, logger, cfg.DryRun)
	result.SwarmsReaped += orphaned
	result.Errors += errs

	return result, nil
}

// openReaperDB opens the Postgres pool the reaper uses for its single run.
// Connection count is intentionally tiny — this is a CronJob.
func openReaperDB(dsn string) (*dbr.Connection, error) {
	conn, err := dbr.Open("pgx", dsn, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	conn.Dialect = dialect.PostgreSQL
	conn.SetMaxOpenConns(reaperMaxOpenConns)
	conn.SetMaxIdleConns(reaperMaxIdleConns)
	return conn, nil
}

// runStaleSwarmPhase is phase 1: list all UserSwarm CRs and reap the ones
// older than cutoff whose owning user is an e2e test user.
func runStaleSwarmPhase(ctx context.Context, k8sClient client.Client, sess *dbr.Session, repo *reaperrepo.Repo, logger *slog.Logger, cfg *Config, cutoff time.Time, result *Result) error {
	var swarmList crawblv1alpha1.UserSwarmList
	if err := k8sClient.List(ctx, &swarmList); err != nil {
		return fmt.Errorf("list userswarms: %w", err)
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
		reapStaleSwarm(ctx, &staleSwarmCtx{
			k8sClient:        k8sClient,
			sess:             sess,
			repo:             repo,
			logger:           logger,
			cfg:              cfg,
			cutoff:           cutoff,
			result:           result,
			softDeletedUsers: softDeletedUsers,
			swarm:            &swarmList.Items[i],
		})
	}
	return nil
}

// staleSwarmCtx bundles the per-swarm inputs so the loop body doesn't need a
// nine-parameter function signature.
type staleSwarmCtx struct {
	k8sClient        client.Client
	sess             *dbr.Session
	repo             *reaperrepo.Repo
	logger           *slog.Logger
	cfg              *Config
	cutoff           time.Time
	result           *Result
	softDeletedUsers map[string]struct{}
	swarm            *crawblv1alpha1.UserSwarm
}

// reapStaleSwarm processes a single CR from phase 1. Work is split into small
// helpers so the outer loop body stays a straight list of guard-clauses.
func reapStaleSwarm(ctx context.Context, sc *staleSwarmCtx) {
	if sc.swarm.CreationTimestamp.After(sc.cutoff) {
		return
	}

	userRow, ok := lookupSwarmUser(ctx, sc)
	if !ok {
		return
	}
	if !strings.HasPrefix(userRow.Subject, e2eUserSubjectPrefix) {
		return
	}

	age := time.Since(sc.swarm.CreationTimestamp.Time).Truncate(time.Minute)
	sc.result.UsersFound++
	sc.logger.Info("processing stale userswarm",
		"swarm", sc.swarm.Name,
		"subject", userRow.Subject,
		"email", userRow.Email,
		"age", age,
		"dry_run", sc.cfg.DryRun,
	)

	if sc.cfg.DryRun {
		reapStaleSwarmDryRun(sc, userRow.ID)
		return
	}

	if !deleteStaleSwarmCR(ctx, sc) {
		return
	}
	softDeleteStaleUser(ctx, sc, userRow)
}

// lookupSwarmUser resolves the owning user row for a stale swarm. Returns
// ok=false when the swarm should be skipped (DB error or user missing).
// Orphans (no user row) are deferred to phase 2 so the log clearly separates
// "stale e2e" from "orphaned".
func lookupSwarmUser(ctx context.Context, sc *staleSwarmCtx) (*reaperrepo.UserRow, bool) {
	userRow, err := sc.repo.FindUserByID(ctx, sc.sess, sc.swarm.Spec.UserID)
	if err != nil {
		sc.logger.Error("failed to look up user for stale swarm",
			"swarm", sc.swarm.Name,
			"user_id", sc.swarm.Spec.UserID,
			"error", err,
		)
		sc.result.Errors++
		return nil, false
	}
	if userRow == nil {
		return nil, false
	}
	return userRow, true
}

func reapStaleSwarmDryRun(sc *staleSwarmCtx, userID string) {
	sc.result.SwarmsReaped++
	if _, seen := sc.softDeletedUsers[userID]; seen {
		return
	}
	sc.softDeletedUsers[userID] = struct{}{}
	sc.result.UsersReaped++
}

// deleteStaleSwarmCR deletes the CR from the cluster. Returns false when a
// real error occurred and the caller must not proceed to user soft-delete.
func deleteStaleSwarmCR(ctx context.Context, sc *staleSwarmCtx) bool {
	if err := sc.k8sClient.Delete(ctx, sc.swarm); err != nil {
		if client.IgnoreNotFound(err) != nil {
			sc.logger.Error("failed to delete stale userswarm", "name", sc.swarm.Name, "error", err)
			sc.result.Errors++
			return false
		}
	}
	sc.logger.Info("deleted stale userswarm", "name", sc.swarm.Name)
	sc.result.SwarmsReaped++
	return true
}

// softDeleteStaleUser soft-deletes the owning user row exactly once per run,
// even when multiple CRs belong to the same user. The deleted_at IS NULL
// guard inside SoftDeleteUser is a second line of defence.
func softDeleteStaleUser(ctx context.Context, sc *staleSwarmCtx, userRow *reaperrepo.UserRow) {
	if _, seen := sc.softDeletedUsers[userRow.ID]; seen {
		return
	}
	sc.softDeletedUsers[userRow.ID] = struct{}{}

	if userRow.DeletedAt != nil {
		// Already soft-deleted out of band (e.g. a previous reaper run that
		// failed after the DB update but before the CR delete). Count as
		// reaped for visibility; skip the UPDATE.
		sc.result.UsersReaped++
		return
	}
	if err := sc.repo.SoftDeleteUser(ctx, sc.sess, userRow.ID); err != nil {
		sc.logger.Error("failed to soft-delete user", "subject", userRow.Subject, "error", err)
		sc.result.Errors++
		return
	}
	sc.result.UsersReaped++
	sc.logger.Info("reaped user", "subject", userRow.Subject)
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
