package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
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
		RunE: func(_ *cobra.Command, _ []string) error {
			if url == "" {
				return fmt.Errorf("missing required --url")
			}

			deadline := time.Now().Add(timeout)
			client := &http.Client{Timeout: 5 * time.Second}
			var lastErr error

			for {
				lastErr = runHealthCheck(client, url)
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
	cmd.Flags().DurationVar(&timeout, "timeout", 15*time.Second, "HTTP request timeout")
	cmd.Flags().DurationVar(&interval, "interval", 2*time.Second, "retry interval while waiting for health")

	return cmd
}

func runHealthCheck(client *http.Client, url string) error {
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("health request failed: %w", err)
	}
	defer resp.Body.Close()

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
