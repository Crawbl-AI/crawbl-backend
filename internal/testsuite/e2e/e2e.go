// Package e2e provides a Cucumber/godog-based end-to-end test suite
// for the Crawbl orchestrator. Tests are defined as .feature files
// in Gherkin syntax and executed against a live environment.
package e2e

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	_ "github.com/lib/pq"
)

// Config holds the configuration for an e2e test run.
type Config struct {
	BaseURL     string
	UID         string
	Email       string
	Name        string
	E2EToken    string
	Verbose     bool
	Timeout     time.Duration
	DatabaseDSN string // postgres DSN for DB assertions (optional)
}

// Results holds the aggregate outcome of a test run.
type Results struct {
	Exit int
}

// Run executes the godog test suite and returns results.
func Run(cfg *Config) *Results {
	featuresDir := findFeaturesDir()

	opts := godog.Options{
		Format:    "pretty",
		Paths:     []string{featuresDir},
		Output:    colors.Colored(os.Stdout),
		Strict:    true,
		Randomize: 0,
	}

	if !cfg.Verbose {
		opts.Format = "progress"
	}

	suite := godog.TestSuite{
		Name: "crawbl-e2e",
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			initScenario(sc, cfg)
		},
		Options: &opts,
	}

	return &Results{Exit: suite.Run()}
}

// PrintResults writes a summary to w.
func PrintResults(w io.Writer, r *Results) {
	fmt.Fprintln(w)
	if r.Exit == 0 {
		fmt.Fprintln(w, "All e2e tests passed.")
	} else {
		fmt.Fprintf(w, "E2e tests failed (exit code %d).\n", r.Exit)
	}
}

func findFeaturesDir() string {
	candidates := []string{
		"internal/testsuite/e2e/features",
		"features",
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	_, filename, _, ok := runtime.Caller(0)
	if ok {
		dir := filepath.Join(filepath.Dir(filename), "features")
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	return "internal/testsuite/e2e/features"
}

// testContext holds per-scenario state shared across step definitions.
type testContext struct {
	cfg   *Config
	http  *http.Client
	db    *sql.DB
	users map[string]*testUser
	saved map[string]string
	// Current response state.
	lastStatus int
	lastBody   []byte
}

type testUser struct {
	alias   string
	subject string
	email   string
	name    string
}

func newTestContext(cfg *Config) *testContext {
	tc := &testContext{
		cfg:   cfg,
		http:  &http.Client{Timeout: cfg.Timeout},
		users: make(map[string]*testUser),
		saved: make(map[string]string),
	}

	if cfg.DatabaseDSN != "" {
		db, err := sql.Open("postgres", cfg.DatabaseDSN)
		if err == nil {
			db.SetMaxOpenConns(2)
			db.SetMaxIdleConns(1)
			tc.db = db
		}
	}

	return tc
}

// cleanupTestUsers deletes all test users created during this scenario
// by calling DELETE /v1/auth/delete for each.
func (tc *testContext) cleanupTestUsers() {
	for alias := range tc.users {
		body := map[string]any{
			"reason":      "e2e-cleanup",
			"description": "automatic post-scenario cleanup",
		}
		_, _ = tc.doRequest("DELETE", "/v1/auth/delete", alias, body)
	}
}

func (tc *testContext) cleanup() {
	if tc.db != nil {
		_ = tc.db.Close()
	}
}

func initScenario(sc *godog.ScenarioContext, cfg *Config) {
	tc := newTestContext(cfg)

	sc.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		// Clean up all test users created during this scenario.
		tc.cleanupTestUsers()
		tc.cleanup()
		return ctx, nil
	})

	registerHTTPSteps(sc, tc)
	registerDBSteps(sc, tc)
	registerUserSteps(sc, tc)
	registerAssertionSteps(sc, tc)
	registerStateSteps(sc, tc)
}
