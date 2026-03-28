package test

import (
	"fmt"
	"os"
	"time"


	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/testsuite/e2e"
)

func newE2ECommand() *cobra.Command {
	var (
		baseURL  string
		uid      string
		email    string
		name     string
		e2eToken string
		verbose  bool
		timeout  time.Duration
	)

	cmd := &cobra.Command{
		Use:   "e2e",
		Short: "Run end-to-end tests against a live orchestrator",
		Long: `Run the full orchestrator e2e test suite against a live environment.

Tests authenticate by sending X-Firebase-UID/Email/Name headers directly
to the orchestrator (via port-forward or internal service URL).`,
		Example: `  # Against dev cluster (port-forward)
  kubectl port-forward svc/orchestrator 7171:7171 -n backend
  crawbl test e2e --base-url http://localhost:7171

  # Against dev cluster (gateway — requires auth middleware bypass)
  crawbl test e2e --base-url https://dev.api.crawbl.com`,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg := &e2e.Config{
				BaseURL:  baseURL,
				UID:      uid,
				Email:    email,
				Name:     name,
				E2EToken: e2eToken,
				Verbose:  verbose,
				Timeout:  timeout,
			}

			results := e2e.Run(cfg)
			e2e.PrintResults(os.Stdout, results)

			if results.Failed > 0 {
				return fmt.Errorf("%d/%d tests failed", results.Failed, results.Total)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&baseURL, "base-url", "http://localhost:7171", "Orchestrator base URL")
	cmd.Flags().StringVar(&uid, "uid", "e2e-test-user", "Firebase UID for test user")
	cmd.Flags().StringVar(&email, "email", "e2e@crawbl.test", "Email for test user")
	cmd.Flags().StringVar(&name, "name", "E2E Test User", "Display name for test user")
	cmd.Flags().StringVar(&e2eToken, "e2e-token", os.Getenv("CRAWBL_E2E_TOKEN"), "Shared secret for gateway auth bypass (or set CRAWBL_E2E_TOKEN)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Print request/response details")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "HTTP request timeout")

	return cmd
}
