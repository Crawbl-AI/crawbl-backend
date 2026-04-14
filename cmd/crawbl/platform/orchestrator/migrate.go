package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/gocraft/dbr/v2"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/spf13/cobra"

	agentruntimetools "github.com/Crawbl-AI/crawbl-backend/internal/agentruntime/tools"
	orchestratorrepo "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo"
	"github.com/Crawbl-AI/crawbl-backend/internal/orchestrator/repo/toolsrepo"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/clickhouse"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/database"
	clickhousemigrations "github.com/Crawbl-AI/crawbl-backend/migrations/clickhouse"
	orchestratormigrations "github.com/Crawbl-AI/crawbl-backend/migrations/orchestrator"
	"github.com/Crawbl-AI/crawbl-backend/migrations/orchestrator/seed"
)

const defaultServiceName = "orchestrator"

func newMigrateCommand() *cobra.Command {
	var serviceName string

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run database migrations and seed catalogs",
		Long:  "Run pending Postgres and ClickHouse migrations, then seed reference catalogs.",
		RunE: func(_ *cobra.Command, _ []string) error {
			logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
			ctx := context.Background()

			// 1. Postgres schema migrations.
			if err := runPostgresMigrations(logger); err != nil {
				return fmt.Errorf("postgres migrations: %w", err)
			}

			// 2. ClickHouse schema migrations (no-op when DSN is unset).
			if err := runClickhouseMigrations(ctx, logger); err != nil {
				return fmt.Errorf("clickhouse migrations: %w", err)
			}

			// 3. Seed reference catalogs (idempotent upserts).
			if err := runSeeds(ctx, logger); err != nil {
				return fmt.Errorf("seed catalogs: %w", err)
			}

			logger.Info("all migrations and seeds complete")
			return nil
		},
	}

	cmd.Flags().StringVar(&serviceName, "svc", defaultServiceName, "Migration service name")

	return cmd
}

// runPostgresMigrations applies pending orchestrator schema migrations.
func runPostgresMigrations(logger *slog.Logger) error {
	dbConfig := database.ConfigFromEnv("CRAWBL_")
	if err := database.EnsureSchema(dbConfig); err != nil {
		return fmt.Errorf("ensure schema: %w", err)
	}

	srcDriver, err := iofs.New(orchestratormigrations.FS, ".")
	if err != nil {
		return fmt.Errorf("create migration source: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", srcDriver, database.BuildDSN(dbConfig, true))
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	defer func() {
		srcErr, dbErr := m.Close()
		if srcErr != nil {
			logger.Warn("migrator: source close error", "error", srcErr.Error())
		}
		if dbErr != nil {
			logger.Warn("migrator: db close error", "error", dbErr.Error())
		}
	}()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("run migrations: %w", err)
	}

	logger.Info("postgres migrations applied")
	return nil
}

// runClickhouseMigrations applies embedded ClickHouse DDL. No-ops when
// CRAWBL_CLICKHOUSE_DSN is unset (analytics disabled).
func runClickhouseMigrations(ctx context.Context, logger *slog.Logger) error {
	clickhouseDB, err := clickhouse.Open(ctx, logger)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	if clickhouseDB == nil {
		logger.Info("clickhouse migrations skipped: not configured")
		return nil
	}
	defer func() { _ = clickhouseDB.Close() }()

	if err := clickhouse.Migrate(ctx, clickhouseDB, clickhousemigrations.FS, logger); err != nil {
		return err
	}
	return nil
}

// runSeeds upserts all reference catalogs into the database. Idempotent —
// safe to run on every deploy. All phases run inside a single transaction.
func runSeeds(ctx context.Context, logger *slog.Logger) error {
	dbConfig := database.ConfigFromEnv("CRAWBL_")
	conn, err := database.New(dbConfig)
	if err != nil {
		return fmt.Errorf("open db for seeds: %w", err)
	}
	defer func() { _ = conn.Close() }()

	return seedCatalogs(ctx, conn, logger)
}

// seedCatalogs upserts all reference catalogs into the database.
// Covers tools, models, tool categories, integration categories,
// integration providers, usage plans, and model pricing.
// All 7 phases run inside a single transaction so a crash mid-seed never
// leaves partial reference data behind.
func seedCatalogs(ctx context.Context, db *dbr.Connection, logger *slog.Logger) error {
	sess := db.NewSession(nil)
	tx, err := sess.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.RollbackUnlessCommitted()

	catalog := agentruntimetools.DefaultCatalog()

	if err := seedTools(ctx, tx, catalog, logger); err != nil {
		return err
	}
	if err := seedModels(ctx, tx); err != nil {
		return err
	}
	if err := seedToolCategories(ctx, tx); err != nil {
		return err
	}
	if err := seedIntegrationCategories(ctx, tx); err != nil {
		return err
	}
	if err := seedIntegrationProviders(ctx, tx); err != nil {
		return err
	}
	if err := seedUsagePlans(ctx, tx); err != nil {
		return err
	}
	if err := seedModelPricing(ctx, tx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	logger.Info("catalogs seeded",
		slog.Int("tools", len(catalog)),
		slog.Int("models", len(seed.AvailableModels())),
		slog.Int("tool_categories", len(agentruntimetools.ToolCategories())),
		slog.Int("integration_categories", len(seed.IntegrationCategories())),
		slog.Int("integration_providers", len(seed.IntegrationProviders())),
		slog.Int("usage_plans", len(seed.UsagePlans())),
		slog.Int("model_pricing", len(seed.ModelPricing())),
	)
	return nil
}

// seedTools upserts the tool catalog rows using the existing repo Seed method.
func seedTools(ctx context.Context, tx *dbr.Tx, catalog []agentruntimetools.ToolDef, logger *slog.Logger) error {
	toolRows := make([]orchestratorrepo.ToolRow, len(catalog))
	for i, t := range catalog {
		toolRows[i] = orchestratorrepo.ToolRow{
			Name:        t.Name,
			DisplayName: t.DisplayName,
			Description: t.Description,
			Category:    string(t.Category),
			IconURL:     t.IconURL,
			SortOrder:   i,
			CreatedAt:   time.Now(),
		}
	}
	repo := toolsrepo.New()
	if mErr := repo.Seed(ctx, tx, toolRows); mErr != nil {
		logger.Error("tool catalog seed failed", "error", mErr.Error())
		return fmt.Errorf("tool catalog seed: %s", mErr.Error())
	}
	return nil
}

// seedModels upserts the available model catalog.
func seedModels(ctx context.Context, tx *dbr.Tx) error {
	for i, m := range seed.AvailableModels() {
		if err := upsertSortedByID(ctx, tx, idKeyedSeed{
			Table:   "models",
			Label:   "model",
			ID:      m.ID,
			SortIdx: i,
			Fields:  map[string]any{"name": m.Name, "description": m.Description},
		}); err != nil {
			return err
		}
	}
	return nil
}

// seedToolCategories upserts the tool category catalog.
func seedToolCategories(ctx context.Context, tx *dbr.Tx) error {
	for i, c := range agentruntimetools.ToolCategories() {
		if err := upsertSortedByID(ctx, tx, idKeyedSeed{
			Table:   "tool_categories",
			Label:   "tool category",
			ID:      string(c.ID),
			SortIdx: i,
			Fields:  map[string]any{"name": c.Name, "image_url": c.ImageURL},
		}); err != nil {
			return err
		}
	}
	return nil
}

// seedIntegrationCategories upserts the integration category catalog.
func seedIntegrationCategories(ctx context.Context, tx *dbr.Tx) error {
	for i, c := range seed.IntegrationCategories() {
		if err := upsertSortedByID(ctx, tx, idKeyedSeed{
			Table:   "integration_categories",
			Label:   "integration category",
			ID:      c.ID,
			SortIdx: i,
			Fields:  map[string]any{"name": c.Name, "image_url": c.ImageURL},
		}); err != nil {
			return err
		}
	}
	return nil
}

// seedIntegrationProviders upserts integration provider rows (update or insert).
func seedIntegrationProviders(ctx context.Context, tx *dbr.Tx) error {
	for i, p := range seed.IntegrationProviders() {
		if err := upsertIntegrationProvider(ctx, tx, i, p); err != nil {
			return err
		}
	}
	return nil
}

// upsertIntegrationProvider updates or inserts a single integration provider row.
func upsertIntegrationProvider(ctx context.Context, tx *dbr.Tx, sortIdx int, p seed.IntegrationEntry) error {
	var existing integrationProviderRow
	err := tx.Select("provider").From("integration_providers").Where("provider = ?", p.Provider).LoadOneContext(ctx, &existing)
	if err != nil && !database.IsRecordNotFoundError(err) {
		return fmt.Errorf("seed integration provider %q: %w", p.Provider, err)
	}
	if existing.Provider != "" {
		_, err = tx.Update("integration_providers").
			Set("name", p.Name).
			Set("description", p.Description).
			Set("icon_url", p.IconURL).
			Set("category_id", p.CategoryID).
			Set("is_enabled", p.IsEnabled).
			Set("sort_order", sortIdx).
			Where("provider = ?", p.Provider).
			ExecContext(ctx)
	} else {
		_, err = tx.InsertInto("integration_providers").
			Pair("provider", p.Provider).
			Pair("name", p.Name).
			Pair("description", p.Description).
			Pair("icon_url", p.IconURL).
			Pair("category_id", p.CategoryID).
			Pair("is_enabled", p.IsEnabled).
			Pair("sort_order", sortIdx).
			Pair("created_at", time.Now()).
			ExecContext(ctx)
	}
	if err != nil {
		return fmt.Errorf("seed integration provider %q: %w", p.Provider, err)
	}
	return nil
}

// seedUsagePlans upserts usage plan rows (update or insert).
func seedUsagePlans(ctx context.Context, tx *dbr.Tx) error {
	for _, p := range seed.UsagePlans() {
		if err := upsertUsagePlan(ctx, tx, p); err != nil {
			return err
		}
	}
	return nil
}

// upsertUsagePlan updates or inserts a single usage plan row.
func upsertUsagePlan(ctx context.Context, tx *dbr.Tx, p seed.UsagePlanEntry) error {
	var existing struct {
		PlanID string `db:"plan_id"`
	}
	err := tx.Select("plan_id").From("usage_plans").
		Where("plan_id = ?", p.PlanID).
		LoadOneContext(ctx, &existing)
	if err != nil && !database.IsRecordNotFoundError(err) {
		return fmt.Errorf("seed usage plan %q: %w", p.PlanID, err)
	}
	if existing.PlanID != "" {
		_, err = tx.Update("usage_plans").
			Set("name", p.Name).
			Set("monthly_token_limit", p.MonthlyTokenLimit).
			Set("daily_request_limit", p.DailyRequestLimit).
			Set("max_tokens_per_request", p.MaxTokensPerRequest).
			Set("updated_at", time.Now()).
			Where("plan_id = ?", p.PlanID).
			ExecContext(ctx)
	} else {
		_, err = tx.InsertInto("usage_plans").
			Pair("plan_id", p.PlanID).
			Pair("name", p.Name).
			Pair("monthly_token_limit", p.MonthlyTokenLimit).
			Pair("daily_request_limit", p.DailyRequestLimit).
			Pair("max_tokens_per_request", p.MaxTokensPerRequest).
			Pair("created_at", time.Now()).
			Pair("updated_at", time.Now()).
			ExecContext(ctx)
	}
	if err != nil {
		return fmt.Errorf("seed usage plan %q: %w", p.PlanID, err)
	}
	return nil
}

// seedModelPricing bootstraps model pricing rows. Rows that already exist
// are skipped — the CronJob is the real source of truth for pricing data.
func seedModelPricing(ctx context.Context, tx *dbr.Tx) error {
	for _, p := range seed.ModelPricing() {
		if err := insertModelPricingIfAbsent(ctx, tx, p); err != nil {
			return err
		}
	}
	return nil
}

// insertModelPricingIfAbsent inserts a model pricing row only when no existing
// row matches (provider, model, region). Existing rows are left untouched so
// CronJob-managed data is not overwritten.
func insertModelPricingIfAbsent(ctx context.Context, tx *dbr.Tx, p seed.ModelPricingEntry) error {
	var existing struct {
		Model string `db:"model"`
	}
	err := tx.Select("model").From("model_pricing").
		Where("provider = ? AND model = ? AND region = ?", p.Provider, p.Model, p.Region).
		OrderBy("effective_at DESC").
		Limit(1).
		LoadOneContext(ctx, &existing)
	if err != nil && !database.IsRecordNotFoundError(err) {
		return fmt.Errorf("seed model pricing %q: %w", p.Model, err)
	}
	if existing.Model != "" {
		return nil // Already has pricing — don't overwrite CronJob data.
	}
	_, err = tx.InsertInto("model_pricing").
		Pair("provider", p.Provider).
		Pair("model", p.Model).
		Pair("region", p.Region).
		Pair("input_cost_per_token", p.InputCostPerToken).
		Pair("output_cost_per_token", p.OutputCostPerToken).
		Pair("cached_cost_per_token", p.CachedCostPerToken).
		Pair("source", p.Source).
		Pair("effective_at", time.Now()).
		Pair("created_at", time.Now()).
		ExecContext(ctx)
	if err != nil {
		return fmt.Errorf("seed model pricing %q: %w", p.Model, err)
	}
	return nil
}

// idKeyedSeed describes a single upsert against a reference table whose
// primary key is a string column named "id" and which carries a sort_order
// position plus a set of other mutable columns.
type idKeyedSeed struct {
	Table   string
	Label   string
	ID      string
	SortIdx int
	Fields  map[string]any
}

// upsertSortedByID upserts one row into an id-keyed reference table. If a
// row with the given ID already exists it updates the mutable Fields and
// sort_order; otherwise it inserts a new row with created_at = now.
// Idempotent.
func upsertSortedByID(ctx context.Context, tx *dbr.Tx, s idKeyedSeed) error {
	var existingID string
	err := tx.Select("id").From(s.Table).Where("id = ?", s.ID).LoadOneContext(ctx, &existingID)
	if err != nil && !database.IsRecordNotFoundError(err) {
		return fmt.Errorf("seed %s %q: %w", s.Label, s.ID, err)
	}

	if existingID != "" {
		upd := tx.Update(s.Table).Set("sort_order", s.SortIdx)
		for col, v := range s.Fields {
			upd = upd.Set(col, v)
		}
		if _, err := upd.Where("id = ?", s.ID).ExecContext(ctx); err != nil {
			return fmt.Errorf("seed %s %q: %w", s.Label, s.ID, err)
		}
		return nil
	}

	ins := tx.InsertInto(s.Table).
		Pair("id", s.ID).
		Pair("sort_order", s.SortIdx).
		Pair("created_at", time.Now())
	for col, v := range s.Fields {
		ins = ins.Pair(col, v)
	}
	if _, err := ins.ExecContext(ctx); err != nil {
		return fmt.Errorf("seed %s %q: %w", s.Label, s.ID, err)
	}
	return nil
}
