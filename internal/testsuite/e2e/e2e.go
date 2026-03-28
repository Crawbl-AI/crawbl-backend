// Package e2e provides the end-to-end test runner for the Crawbl orchestrator.
// Tests run against a live environment using httpexpect for fluent HTTP assertions.
package e2e

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gavv/httpexpect/v2"
)

// Config holds the configuration for an e2e test run.
type Config struct {
	BaseURL  string
	UID      string
	Email    string
	Name     string
	E2EToken string // shared secret for gateway auth bypass; empty = direct/port-forward mode
	Verbose  bool
	Timeout  time.Duration
}

// Results holds the aggregate outcome of a test run.
type Results struct {
	Total   int
	Passed  int
	Failed  int
	Cases   []CaseResult
}

// CaseResult holds the outcome of a single test case.
type CaseResult struct {
	Suite   string
	Name    string
	Passed  bool
	Error   string
	Elapsed time.Duration
}

// Suite groups related test cases under a name.
type Suite struct {
	Name  string
	Tests []Test
}

// Test is a named test case within a suite.
type Test struct {
	Name string
	Fn   TestFunc
}

// TestFunc is a single e2e test. It receives an httpexpect instance with
// auth headers pre-configured, a public (no-auth) instance, and shared state.
type TestFunc func(auth *httpexpect.Expect, pub *httpexpect.Expect, state map[string]string)

// Run executes all e2e test suites and returns aggregate results.
func Run(cfg *Config) *Results {
	reporter := &Reporter{}
	state := map[string]string{}

	printers := []httpexpect.Printer{}
	if cfg.Verbose {
		printers = append(printers, httpexpect.NewCompactPrinter(&verboseLogger{}))
	}

	// Authenticated client — injects X-Firebase-UID/Email/Name on every request.
	auth := httpexpect.WithConfig(httpexpect.Config{
		BaseURL:  cfg.BaseURL,
		Reporter: reporter,
		Printers: printers,
		Client: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &authTransport{
				base:     http.DefaultTransport,
				uid:      cfg.UID,
				email:    cfg.Email,
				name:     cfg.Name,
				e2eToken: cfg.E2EToken,
			},
		},
	})

	// Public client — no auth headers.
	pub := httpexpect.WithConfig(httpexpect.Config{
		BaseURL:  cfg.BaseURL,
		Reporter: reporter,
		Printers: printers,
		Client:   &http.Client{Timeout: cfg.Timeout},
	})

	suites := []Suite{
		SuiteHealth(),
		SuiteAuth(cfg),
		SuiteProfile(),
		SuiteLegal(),
		SuiteWorkspaces(),
		SuiteChat(cfg),
		SuiteCleanup(),
	}

	results := &Results{}

	for _, suite := range suites {
		fmt.Printf("\n--- %s ---\n", suite.Name)
		for _, t := range suite.Tests {
			reporter.Reset()
			start := time.Now()
			t.Fn(auth, pub, state)
			elapsed := time.Since(start)

			cr := CaseResult{
				Suite:   suite.Name,
				Name:    t.Name,
				Elapsed: elapsed,
			}

			if reporter.Failed() {
				cr.Passed = false
				cr.Error = reporter.Error()
				results.Failed++
				fmt.Printf("  FAIL %s: %s\n", t.Name, cr.Error)
			} else {
				cr.Passed = true
				results.Passed++
				fmt.Printf("  PASS %s (%s)\n", t.Name, elapsed.Truncate(time.Millisecond))
			}

			results.Cases = append(results.Cases, cr)
			results.Total++
		}
	}

	return results
}

// PrintResults writes a human-readable test report to w.
func PrintResults(w io.Writer, r *Results) {
	fmt.Fprintln(w)
	fmt.Fprintf(w, "=== E2E Test Results ===\n")
	fmt.Fprintf(w, "Total: %d  Passed: %d  Failed: %d\n\n", r.Total, r.Passed, r.Failed)

	for _, c := range r.Cases {
		status := "PASS"
		if !c.Passed {
			status = "FAIL"
		}
		fmt.Fprintf(w, "  [%s] %s/%s (%s)\n", status, c.Suite, c.Name, c.Elapsed.Truncate(time.Millisecond))
		if c.Error != "" {
			fmt.Fprintf(w, "         %s\n", c.Error)
		}
	}

	fmt.Fprintln(w)
	if r.Failed == 0 {
		fmt.Fprintln(w, "All tests passed.")
	} else {
		fmt.Fprintf(w, "%d test(s) failed.\n", r.Failed)
	}
}

// verboseLogger implements httpexpect.Logger for compact printer output.
type verboseLogger struct{}

func (l *verboseLogger) Logf(format string, args ...interface{}) {
	fmt.Printf("  "+format+"\n", args...)
}
