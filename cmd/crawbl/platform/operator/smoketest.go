package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
)

const (
	defaultHTTPClientTimeout   = 5 * time.Second
	defaultSmokeTestTimeout    = 15 * time.Second
	defaultHealthCheckInterval = 2 * time.Second
)

type healthResponse struct {
	Status string `json:"status"`
}

func newSmokeTestCommand() *cobra.Command {
	var (
		url      string
		timeout  time.Duration
		interval time.Duration
	)

	cmd := &cobra.Command{
		Use:   "smoketest",
		Short: "Verify the UserSwarm service path through the health endpoint",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if url == "" {
				return fmt.Errorf("missing required --url")
			}

			ctx := cmd.Context()
			deadline := time.Now().Add(timeout)
			client := &http.Client{Timeout: defaultHTTPClientTimeout}
			var lastErr error

			for {
				lastErr = runHealthCheck(ctx, client, url)
				if lastErr == nil {
					_, _ = fmt.Fprintln(os.Stdout, "smoke test passed")
					return nil
				}
				if time.Now().After(deadline) {
					return fmt.Errorf("smoke test failed: %w", lastErr)
				}
				time.Sleep(interval)
			}
		},
	}

	cmd.Flags().StringVar(&url, "url", "", "HTTP health endpoint to verify")
	cmd.Flags().DurationVar(&timeout, "timeout", defaultSmokeTestTimeout, "HTTP request timeout")
	cmd.Flags().DurationVar(&interval, "interval", defaultHealthCheckInterval, "retry interval while waiting for health")

	return cmd
}

func runHealthCheck(ctx context.Context, client *http.Client, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create health request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("health request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected health status code: %d", resp.StatusCode)
	}

	var payload healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fmt.Errorf("failed to decode health response: %w", err)
	}
	if payload.Status != "ok" {
		return fmt.Errorf("unexpected health payload status: %q", payload.Status)
	}

	return nil
}
