package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

type healthResponse struct {
	Status string `json:"status"`
}

func main() {
	var (
		url      string
		timeout  time.Duration
		interval time.Duration
	)

	flag.StringVar(&url, "url", "", "HTTP health endpoint to verify")
	flag.DurationVar(&timeout, "timeout", 15*time.Second, "HTTP request timeout")
	flag.DurationVar(&interval, "interval", 2*time.Second, "retry interval while waiting for health")
	flag.Parse()

	if strings.TrimSpace(url) == "" {
		fmt.Fprintln(os.Stderr, "missing required --url")
		os.Exit(1)
	}

	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 5 * time.Second}
	var lastErr error

	for {
		lastErr = runHealthCheck(client, url)
		if lastErr == nil {
			fmt.Println("smoke test passed")
			return
		}
		if time.Now().After(deadline) {
			fmt.Fprintf(os.Stderr, "smoke test failed: %v\n", lastErr)
			os.Exit(1)
		}
		time.Sleep(interval)
	}
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
