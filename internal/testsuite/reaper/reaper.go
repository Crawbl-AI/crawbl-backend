// Package reaper provides cleanup of orphaned e2e test resources.
// It queries the database for stale test users (subject LIKE 'e2e-%')
// and deletes their UserSwarm CRs and database records.
package reaper

import (
	"context"
	"fmt"
	"log/slog"
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

// Config holds the configuration for a reaper run.
type Config struct {
	DatabaseDSN string
	MaxAge      time.Duration
	DryRun      bool
}

// staleUser holds the minimal fields needed to clean up a test user.
type staleUser struct {
	ID        string
	Subject   string
	Email     string
	CreatedAt time.Time
}

// Result holds the outcome of a reaper run.
type Result struct {
	UsersFound   int
	UsersReaped  int
	SwarmsReaped int
	Errors       int
}

// Run executes the reaper and returns a summary.
func Run(ctx context.Context, cfg *Config) (*Result, error) {
	logger := slog.Default()
	result := &Result{}

	// Connect to Postgres.
	conn, err := dbr.Open("postgres", cfg.DatabaseDSN, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	defer conn.Close()
	conn.Dialect = dialect.PostgreSQL
	conn.SetMaxOpenConns(2)
	conn.SetMaxIdleConns(1)
	sess := conn.NewSession(nil)

	// Build K8s client for UserSwarm CRD operations.
	k8sClient, err := buildK8sClient()
	if err != nil {
		return nil, fmt.Errorf("build k8s client: %w", err)
	}

	// Find stale e2e users.
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

	for _, user := range users {
		age := time.Since(user.CreatedAt).Truncate(time.Minute)
		logger.Info("processing stale user",
			"subject", user.Subject,
			"email", user.Email,
			"age", age,
			"dry_run", cfg.DryRun,
		)

		if cfg.DryRun {
			result.UsersReaped++
			continue
		}

		// Find and delete UserSwarm CRs for this user's workspaces.
		reaped, errs := reapUserSwarms(ctx, k8sClient, sess, logger, user)
		result.SwarmsReaped += reaped
		result.Errors += errs

		// Soft-delete the user.
		if err := softDeleteUser(ctx, sess, user); err != nil {
			logger.Error("failed to soft-delete user", "subject", user.Subject, "error", err)
			result.Errors++
			continue
		}

		result.UsersReaped++
		logger.Info("reaped user", "subject", user.Subject)
	}

	// Scan for orphaned UserSwarm CRs not tied to any active user.
	orphaned, errs := reapOrphanedSwarms(ctx, k8sClient, sess, logger, cfg.DryRun)
	result.SwarmsReaped += orphaned
	result.Errors += errs

	return result, nil
}

// findStaleE2EUsers queries for e2e test users created before the cutoff that haven't been deleted.
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

// reapUserSwarms finds workspaces for a user and deletes their UserSwarm CRs.
func reapUserSwarms(ctx context.Context, k8sClient client.Client, sess *dbr.Session, logger *slog.Logger, user staleUser) (reaped, errors int) {
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
		var swarm crawblv1alpha1.UserSwarm
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: swarmName}, &swarm); err != nil {
			if client.IgnoreNotFound(err) == nil {
				continue // already gone
			}
			logger.Error("failed to get userswarm", "name", swarmName, "error", err)
			errors++
			continue
		}

		if err := k8sClient.Delete(ctx, &swarm); err != nil {
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

// reapOrphanedSwarms finds UserSwarm CRs whose owning user has been soft-deleted
// or no longer exists in the database.
func reapOrphanedSwarms(ctx context.Context, k8sClient client.Client, sess *dbr.Session, logger *slog.Logger, dryRun bool) (reaped, errors int) {
	var swarmList crawblv1alpha1.UserSwarmList
	if err := k8sClient.List(ctx, &swarmList); err != nil {
		logger.Error("failed to list userswarms", "error", err)
		return 0, 1
	}

	for i := range swarmList.Items {
		swarm := &swarmList.Items[i]
		userID := swarm.Spec.UserID
		if userID == "" {
			continue
		}

		// Check if the user still exists and is not deleted.
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
			continue // user is alive, swarm is valid
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

// softDeleteUser sets deleted_at on the user record.
func softDeleteUser(ctx context.Context, sess *dbr.Session, user staleUser) error {
	now := time.Now().UTC()
	_, err := sess.Update("users").
		Set("deleted_at", now).
		Set("updated_at", now).
		Where("id = ? AND deleted_at IS NULL", user.ID).
		ExecContext(ctx)
	return err
}

// buildK8sClient creates a controller-runtime client configured for UserSwarm CRDs.
func buildK8sClient() (client.Client, error) {
	scheme := runtime.NewScheme()
	utilruntime.Must(crawblv1alpha1.AddToScheme(scheme))
	utilruntime.Must(metav1.AddMetaToScheme(scheme))

	restConfig, err := config.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("get kubeconfig: %w", err)
	}

	return client.New(restConfig, client.Options{Scheme: scheme})
}
