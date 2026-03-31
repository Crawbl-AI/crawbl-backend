// Package e2e provides a Cucumber/godog-based end-to-end test suite
// for the Crawbl orchestrator. Tests are defined as .feature files
// in Gherkin syntax and executed against a live environment.
//
// The suite uses exactly 3 test users across ALL scenarios:
//   - primary: shared across most scenarios (auth, profile, legal, workspaces, chat)
//   - frank: used for multi-user isolation tests
//   - grace: used for multi-user isolation tests
//
// This prevents UserSwarm explosion in the cluster (max 3 UserSwarms per run).
package e2e

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	backendruntime "github.com/Crawbl-AI/crawbl-backend/internal/pkg/runtime"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	"github.com/gocraft/dbr/v2"
	"github.com/gocraft/dbr/v2/dialect"
	_ "github.com/lib/pq"
)

// Config holds the configuration for an e2e test run.
type Config struct {
	BaseURL             string
	UID                 string
	Email               string
	Name                string
	E2EToken            string
	Verbose             bool
	Timeout             time.Duration
	RuntimeReadyTimeout time.Duration
	RuntimePollInterval time.Duration
	DatabaseDSN         string
}

// Results holds the aggregate outcome of a test run.
type Results struct {
	Exit int
}

// suiteUsers holds the 3 fixed test users created once per suite run.
type suiteUsers struct {
	primary *testUser
	frank   *testUser
	grace   *testUser
}

// Run executes the godog test suite and returns results.
func Run(cfg *Config) *Results {
	featuresDir := findFeaturesDir()

	// Create exactly 3 test users for the entire suite run.
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	users := &suiteUsers{
		primary: &testUser{
			alias:   "primary",
			subject: fmt.Sprintf("e2e-primary-%s", suffix),
			email:   fmt.Sprintf("e2e-primary-%s@crawbl.test", suffix),
			name:    "E2E Primary",
		},
		frank: &testUser{
			alias:   "frank",
			subject: fmt.Sprintf("e2e-frank-%s", suffix),
			email:   fmt.Sprintf("e2e-frank-%s@crawbl.test", suffix),
			name:    "E2E Frank",
		},
		grace: &testUser{
			alias:   "grace",
			subject: fmt.Sprintf("e2e-grace-%s", suffix),
			email:   fmt.Sprintf("e2e-grace-%s@crawbl.test", suffix),
			name:    "E2E Grace",
		},
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

	allUsers := []*testUser{users.primary, users.frank, users.grace}

	// Layer 3: use RunUntilSignal for graceful cleanup on SIGINT/SIGTERM.
	// When CI cancels or user hits Ctrl+C, the stop function cleans up
	// all test users before the process exits.
	var exit int
	cleanupFn := func(_ context.Context) error {
		log.Println("cleaning up test users...")
		for _, u := range allUsers {
			cleanupUser(cfg, u)
		}
		log.Println("cleanup done")
		return nil
	}

	runErr := backendruntime.RunUntilSignal(func() error {
		suite := godog.TestSuite{
			Name: "crawbl-e2e",
			ScenarioInitializer: func(sc *godog.ScenarioContext) {
				initScenario(sc, cfg, users)
			},
			Options: &opts,
		}
		exit = suite.Run()
		return nil
	}, cleanupFn, 10*time.Second)

	if runErr != nil {
		// Signal received — cleanup already ran in cleanupFn.
		return &Results{Exit: 1}
	}

	// Normal cleanup after suite completes.
	for _, u := range allUsers {
		cleanupUser(cfg, u)
	}

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
		"test-features",
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
	return "test-features"
}

// testContext holds per-scenario state shared across step definitions.
type testContext struct {
	cfg    *Config
	http   *http.Client
	dbConn *dbr.Connection
	users  map[string]*testUser
	saved  map[string]string
	state  map[string]*userJourneyState
	// Current response state.
	lastStatus int
	lastBody   []byte
}

// testUser represents a test user.
type testUser struct {
	alias    string
	subject  string
	email    string
	name     string
	signedUp bool // track if this user has signed up in the current suite run
}

type userJourneyState struct {
	workspaceID          string
	workspaceName        string
	currentConversation  string
	swarmConversationID  string
	agentIDsBySlug       map[string]string
	agentNamesBySlug     map[string]string
	conversationIDsByKey map[string]string
	pushToken            string
}

func newTestContext(cfg *Config, users *suiteUsers) *testContext {
	tc := &testContext{
		cfg:   cfg,
		http:  &http.Client{Timeout: cfg.Timeout},
		users: make(map[string]*testUser),
		saved: make(map[string]string),
		state: make(map[string]*userJourneyState),
	}

	// All 3 users are available in every scenario.
	tc.users["primary"] = users.primary
	tc.users["frank"] = users.frank
	tc.users["grace"] = users.grace
	// Also register "zach" as an alias for "frank" for cleanup scenarios.
	tc.users["zach"] = users.frank

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

func (tc *testContext) cleanup() {
	if tc.dbConn != nil {
		_ = tc.dbConn.Close()
	}
}

func initScenario(sc *godog.ScenarioContext, cfg *Config, users *suiteUsers) {
	tc := newTestContext(cfg, users)

	sc.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		tc.cleanup()
		return ctx, nil
	})

	registerHTTPSteps(sc, tc)
	registerDBSteps(sc, tc)
	registerUserSteps(sc, tc)
	registerAssertionSteps(sc, tc)
	registerStateSteps(sc, tc)
	registerProductSteps(sc, tc)
}
