package test

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/testsuite/e2e"
)

// envInt parses an int from an env var, returning the fallback on
// missing / malformed values.
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

// portForward represents a running kubectl port-forward subprocess.
type portForward struct {
	cmd       *exec.Cmd
	localPort int
	label     string
}

// startPortForwards launches kubectl port-forward subprocesses for the
// orchestrator, postgres, and redis services in the backend namespace.
// Returns a cleanup function that kills all subprocesses.
func startPortForwards() (orchestratorPort, pgPort, redisPort int, cleanup func(), err error) {
	forwards := []struct {
		svc        string
		localPort  int
		remotePort int
		label      string
	}{
		{"svc/orchestrator", 7171, 7171, "orchestrator"},
		{"svc/backend-postgresql", 5432, 5432, "postgres"},
		{"svc/backend-redis-master", 6379, 6379, "redis"},
	}

	var running []*portForward
	cleanup = func() {
		for _, pf := range running {
			if pf.cmd.Process != nil {
				_ = pf.cmd.Process.Kill()
				_ = pf.cmd.Wait()
			}
		}
	}

	for _, f := range forwards {
		cmd := exec.CommandContext(context.Background(), "kubectl", "port-forward", f.svc,
			fmt.Sprintf("%d:%d", f.localPort, f.remotePort),
			"-n", "backend")
		cmd.Stdout = nil
		cmd.Stderr = nil
		if startErr := cmd.Start(); startErr != nil {
			cleanup()
			return 0, 0, 0, nil, fmt.Errorf("failed to start port-forward for %s: %w", f.label, startErr)
		}
		running = append(running, &portForward{cmd: cmd, localPort: f.localPort, label: f.label})
	}

	// Wait for all ports to become reachable (up to 10 seconds).
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, pf := range running {
		if waitErr := waitForPort(ctx, pf.localPort); waitErr != nil {
			cleanup()
			return 0, 0, 0, nil, fmt.Errorf("port-forward for %s (:%d) did not become ready: %w", pf.label, pf.localPort, waitErr)
		}
		log.Printf("port-forward ready: %s → localhost:%d", pf.label, pf.localPort)
	}

	return forwards[0].localPort, forwards[1].localPort, forwards[2].localPort, cleanup, nil
}

// dialTimeout is the per-attempt TCP dial timeout used by waitForPort.
const dialTimeout = 500 * time.Millisecond

// portPollInterval is how often waitForPort retries between dial attempts.
const portPollInterval = 300 * time.Millisecond

// waitForPort polls a TCP port until it accepts connections or ctx expires.
func waitForPort(ctx context.Context, port int) error {
	addr := fmt.Sprintf("localhost:%d", port)
	dialer := &net.Dialer{Timeout: dialTimeout}
	ticker := time.NewTicker(portPollInterval)
	defer ticker.Stop()
	for {
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func newE2ECommand() *cobra.Command {
	var (
		baseURL             string
		e2eToken            string
		verbose             bool
		timeout             time.Duration
		runtimeReadyTimeout time.Duration
		runtimePollInterval time.Duration
		databaseDSN         string
		category            string
		tags                string
		portForwardFlag     bool
	)

	cmd := &cobra.Command{
		Use:   "e2e",
		Short: "Run end-to-end tests against a live environment",
		Long: `Run the full orchestrator e2e test suite (Cucumber/godog) against a live environment.

Tests authenticate via X-E2E-Token (gateway mode) or X-Firebase-UID (port-forward mode).
Database assertions require --database-dsn to connect to the orchestrator's Postgres.

Use --port-forward to automatically set up kubectl port-forwards for orchestrator,
postgres, and redis before running tests. They are cleaned up when the run finishes.

Use --category to run only tests from a specific subfolder (e.g. --category chat
runs only test-features/chat/).`,
		Example: `  # Auto port-forward + run all tests
  crawbl test e2e --port-forward --verbose

  # Run only chat tests
  crawbl test e2e --port-forward --category chat --verbose

  # Run only tools tests
  crawbl test e2e --port-forward --category tools --verbose

  # Gateway mode (CI) — no port-forward needed
  crawbl test e2e --base-url https://dev.api.crawbl.com --e2e-token $CRAWBL_E2E_TOKEN`,
		RunE: func(_ *cobra.Command, _ []string) error {
			var pfCleanup func()
			if portForwardFlag {
				log.Println("setting up port-forwards...")
				orchPort, pgPort, redisPort, cleanup, err := startPortForwards()
				if err != nil {
					return fmt.Errorf("port-forward setup failed: %w", err)
				}
				pfCleanup = cleanup
				defer pfCleanup()

				// Override base-url if still default and port-forward is active.
				if baseURL == "http://localhost:7171" || baseURL == "" {
					baseURL = fmt.Sprintf("http://localhost:%d", orchPort)
				}

				// Auto-configure infra clients from cluster secrets when
				// port-forward is active and env vars are not already set.
				if os.Getenv("CRAWBL_E2E_REDIS_ADDR") == "" {
					_ = os.Setenv("CRAWBL_E2E_REDIS_ADDR", fmt.Sprintf("localhost:%d", redisPort))
				}

				// Build DSN if not provided and the PG password env is set.
				if databaseDSN == "" {
					pgPass := os.Getenv("CRAWBL_E2E_PG_PASSWORD")
					if pgPass != "" {
						databaseDSN = fmt.Sprintf("postgres://postgres:%s@localhost:%d/crawbl?sslmode=disable&search_path=orchestrator", pgPass, pgPort)
					}
				}
			}

			cfg := &e2e.Config{
				BaseURL:             baseURL,
				E2EToken:            e2eToken,
				Verbose:             verbose,
				Timeout:             timeout,
				RuntimeReadyTimeout: runtimeReadyTimeout,
				RuntimePollInterval: runtimePollInterval,
				DatabaseDSN:         databaseDSN,
				Category:            category,
				Tags:                tags,

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
	cmd.Flags().StringVar(&e2eToken, "e2e-token", os.Getenv("CRAWBL_E2E_TOKEN"), "Shared secret for gateway auth bypass, or set CRAWBL_E2E_TOKEN")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Print detailed test output")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "HTTP request timeout (includes agent tool-call latency and runtime cold-start)")
	cmd.Flags().DurationVar(&runtimeReadyTimeout, "runtime-ready-timeout", 3*time.Minute, "How long to wait for a workspace runtime to become ready before chat scenarios fail")
	cmd.Flags().DurationVar(&runtimePollInterval, "runtime-poll-interval", 2*time.Second, "How often to poll workspace runtime readiness during chat scenarios")
	cmd.Flags().StringVar(&databaseDSN, "database-dsn", os.Getenv("CRAWBL_E2E_DATABASE_DSN"), "Postgres DSN for database assertions")
	cmd.Flags().StringVar(&category, "category", "", "Run only tests from a specific subfolder (e.g. chat, tools, auth, mcp)")
	cmd.Flags().StringVar(&tags, "tags", "", "Godog tag filter expression (e.g. \"~@llm-flaky\" to skip flaky scenarios, \"@smoke\" to run only smoke tests)")
	cmd.Flags().BoolVar(&portForwardFlag, "port-forward", false, "Auto-start kubectl port-forwards for orchestrator, postgres, and redis")

	// Hidden aliases for common shortcuts.
	cmd.Flags().StringVarP(&category, "cat", "c", "", "Alias for --category")
	_ = cmd.Flags().MarkHidden("cat")

	return cmd
}
