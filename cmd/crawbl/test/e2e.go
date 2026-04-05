package test

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/testsuite/e2e"
)

// envInt parses an int from an env var, returning the fallback on
// missing / malformed values. Used for CRAWBL_E2E_REDIS_DB so CI
// never has to pass it explicitly.
func envInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

func newE2ECommand() *cobra.Command {
	var (
		baseURL             string
		uid                 string
		email               string
		name                string
		e2eToken            string
		verbose             bool
		timeout             time.Duration
		runtimeReadyTimeout time.Duration
		runtimePollInterval time.Duration
		databaseDSN         string
	)

	cmd := &cobra.Command{
		Use:   "e2e",
		Short: "Run end-to-end tests against a live environment",
		Long: `Run the full orchestrator e2e test suite (Cucumber/godog) against a live environment.

Tests authenticate via X-E2E-Token (gateway mode) or X-Firebase-UID (port-forward mode).
Database assertions require --database-dsn to connect to the orchestrator's Postgres.`,
		Example: `  # Port-forward mode (no e2e token needed)
  kubectl port-forward svc/orchestrator 7171:7171 -n backend
  crawbl test e2e --base-url http://localhost:7171

  # Gateway mode with DB assertions
  crawbl test e2e \
    --base-url https://dev.api.crawbl.com \
    --e2e-token $CRAWBL_E2E_TOKEN \
    --database-dsn "postgres://user:pass@host:5432/crawbl?sslmode=disable&search_path=orchestrator"`,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg := &e2e.Config{
				BaseURL:             baseURL,
				UID:                 uid,
				Email:               email,
				Name:                name,
				E2EToken:            e2eToken,
				Verbose:             verbose,
				Timeout:             timeout,
				RuntimeReadyTimeout: runtimeReadyTimeout,
				RuntimePollInterval: runtimePollInterval,
				DatabaseDSN:         databaseDSN,

				// Infrastructure clients are env-only so CI keeps
				// invoking `crawbl test e2e --base-url ...` unchanged.
				// When these are unset the corresponding scenarios
				// skip their assertions gracefully.
				RedisAddr:       os.Getenv("CRAWBL_E2E_REDIS_ADDR"),
				RedisPassword:   os.Getenv("CRAWBL_E2E_REDIS_PASSWORD"),
				RedisDB:         envInt("CRAWBL_E2E_REDIS_DB", 0),
				SpacesEndpoint:  os.Getenv("CRAWBL_E2E_SPACES_ENDPOINT"),
				SpacesRegion:    os.Getenv("CRAWBL_E2E_SPACES_REGION"),
				SpacesBucket:    os.Getenv("CRAWBL_E2E_SPACES_BUCKET"),
				SpacesAccessKey: os.Getenv("CRAWBL_E2E_SPACES_ACCESS_KEY"),
				SpacesSecretKey: os.Getenv("CRAWBL_E2E_SPACES_SECRET_KEY"),
			}

			results := e2e.Run(cfg)
			e2e.PrintResults(os.Stdout, results)

			if results.Exit != 0 {
				return fmt.Errorf("e2e tests failed (exit %d)", results.Exit)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&baseURL, "base-url", "http://localhost:7171", "Orchestrator base URL")
	cmd.Flags().StringVar(&uid, "uid", "e2e-test-user", "Firebase UID for the test user")
	cmd.Flags().StringVar(&email, "email", "e2e@crawbl.test", "Email for the test user")
	cmd.Flags().StringVar(&name, "name", "E2E Test User", "Display name for the test user")
	cmd.Flags().StringVar(&e2eToken, "e2e-token", os.Getenv("CRAWBL_E2E_TOKEN"), "Shared secret for gateway auth bypass, or set CRAWBL_E2E_TOKEN")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Print detailed test output")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "HTTP request timeout")
	cmd.Flags().DurationVar(&runtimeReadyTimeout, "runtime-ready-timeout", 3*time.Minute, "How long to wait for a workspace runtime to become ready before chat scenarios fail")
	cmd.Flags().DurationVar(&runtimePollInterval, "runtime-poll-interval", 2*time.Second, "How often to poll workspace runtime readiness during chat scenarios")
	cmd.Flags().StringVar(&databaseDSN, "database-dsn", os.Getenv("CRAWBL_E2E_DATABASE_DSN"), "Postgres DSN for database assertions")

	return cmd
}
