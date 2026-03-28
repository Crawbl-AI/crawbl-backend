// Package e2e provides a Cucumber/godog-based end-to-end test suite
// for the Crawbl orchestrator. Tests are defined as .feature files
// in Gherkin syntax and executed against a live environment.
//
// The suite uses a single shared test user across all scenarios to avoid
// creating excessive UserSwarm CRs in the cluster. Multi-user isolation
// tests create additional users but are capped at 3 total.
package e2e

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	"github.com/gocraft/dbr/v2"
	"github.com/gocraft/dbr/v2/dialect"
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

	// Create the shared primary test user once for the entire suite.
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	primary := &testUser{
		alias:   "primary",
		subject: fmt.Sprintf("e2e-primary-%s", suffix),
		email:   fmt.Sprintf("e2e-primary-%s@crawbl.test", suffix),
		name:    "E2E Primary",
	}

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
			initScenario(sc, cfg, primary)
		},
		Options: &opts,
	}

	exit := suite.Run()

	// Clean up primary user after entire suite.
	cleanupUser(cfg, primary)

	return &Results{Exit: exit}
}

// cleanupUser deletes a test user via the API.
func cleanupUser(cfg *Config, user *testUser) {
	client := &http.Client{Timeout: cfg.Timeout}
	tc := &testContext{
		cfg:   cfg,
		http:  client,
		users: map[string]*testUser{user.alias: user},
	}
	body := map[string]any{
		"reason":      "e2e-cleanup",
		"description": "suite-level cleanup",
	}
	_, _ = tc.doRequest("DELETE", "/v1/auth/delete", user.alias, body)
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
	cfg    *Config
	http   *http.Client
	dbConn *dbr.Connection
	users  map[string]*testUser
	saved  map[string]string
	// extraUsers tracks users created in this scenario (not the primary).
	// These get deleted in the After hook.
	extraUsers []string
	// Current response state.
	lastStatus int
	lastBody   []byte
}

// testUser represents a test user.
type testUser struct {
	alias   string
	subject string
	email   string
	name    string
}

func newTestContext(cfg *Config, primary *testUser) *testContext {
	tc := &testContext{
		cfg:   cfg,
		http:  &http.Client{Timeout: cfg.Timeout},
		users: make(map[string]*testUser),
		saved: make(map[string]string),
	}

	// The primary user is shared across all scenarios.
	tc.users["primary"] = primary

	if cfg.DatabaseDSN != "" {
		conn, err := dbr.Open("postgres", cfg.DatabaseDSN, nil)
		if err == nil {
			conn.Dialect = dialect.PostgreSQL
			conn.SetMaxOpenConns(2)
			conn.SetMaxIdleConns(1)
			tc.dbConn = conn
		}
	}

	return tc
}

// cleanupExtraUsers deletes users created during this scenario (not primary).
func (tc *testContext) cleanupExtraUsers() {
	for _, alias := range tc.extraUsers {
		body := map[string]any{
			"reason":      "e2e-cleanup",
			"description": "post-scenario cleanup",
		}
		_, _ = tc.doRequest("DELETE", "/v1/auth/delete", alias, body)
	}
}

func (tc *testContext) cleanup() {
	if tc.dbConn != nil {
		_ = tc.dbConn.Close()
	}
}

func initScenario(sc *godog.ScenarioContext, cfg *Config, primary *testUser) {
	tc := newTestContext(cfg, primary)

	sc.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		tc.cleanupExtraUsers()
		tc.cleanup()
		return ctx, nil
	})

	registerHTTPSteps(sc, tc)
	registerDBSteps(sc, tc)
	registerUserSteps(sc, tc)
	registerAssertionSteps(sc, tc)
	registerStateSteps(sc, tc)
}
