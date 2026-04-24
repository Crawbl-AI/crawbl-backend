// Package e2e provides a Cucumber/godog-based end-to-end test suite
// for the Crawbl orchestrator. Tests are defined as .feature files
// in Gherkin syntax and executed against a live environment.
//
// The suite uses exactly 4 test users across ALL scenarios:
//   - primary: shared across most scenarios (auth, profile, legal, workspaces, chat)
//   - frank: used for multi-user isolation tests
//   - grace: used for multi-user isolation tests
//   - zach: dedicated user for account deletion / cleanup scenarios
//
// This prevents runtime instance explosion in the cluster (max 3 runtime instances per run).
package e2e

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	mobilev1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mobile/v1"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	"golang.org/x/sync/errgroup"
)

// buildTags assembles the godog tag expression, always excluding @wip and
// @llm-flaky unless the caller's expression already mentions them. It also
// excludes @db scenarios when no database DSN is configured.
func buildTags(cfg *Config) string {
	tags := cfg.Tags
	if tags == "" {
		tags = "~@wip && ~@llm-flaky"
	} else {
		if !strings.Contains(tags, "@wip") {
			tags += " && ~@wip"
		}
		if !strings.Contains(tags, "@llm-flaky") {
			tags += " && ~@llm-flaky"
		}
	}
	if cfg.DatabaseDSN == "" {
		tags += " && ~@db"
	}
	return tags
}

// Run executes the godog test suite and returns results.
func Run(cfg *Config) *Results {
	featuresDir := findFeaturesDir()
	if cfg.Category != "" {
		featuresDir = filepath.Join(featuresDir, cfg.Category)
		if info, err := os.Stat(featuresDir); err != nil || !info.IsDir() {
			return &Results{Exit: 1}
		}
	}

	// Create exactly 4 test users for the entire suite run.
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
		zach: &testUser{
			alias:   "zach",
			subject: fmt.Sprintf("e2e-zach-%s", suffix),
			email:   fmt.Sprintf("e2e-zach-%s@crawbl.test", suffix),
			name:    "E2E Zach",
		},
	}

	tags := buildTags(cfg)

	opts := godog.Options{
		Format:    "pretty",
		Paths:     []string{featuresDir},
		Output:    colors.Colored(os.Stdout),
		Strict:    true,
		Randomize: 0,
		Tags:      tags,
	}

	if !cfg.Verbose {
		opts.Format = "progress"
	}

	allUsers := []*testUser{users.primary, users.frank, users.grace, users.zach}

	deps := newSuiteDeps(cfg)
	defer deps.close()

	// Hard-purge any residue from previous runs before a new suite
	// starts. The API /v1/auth/delete only soft-deletes users, so
	// without this every iteration leaks ~9 agents + 3 workspaces
	// + 4 memory_* rows into the dev DB. Safe no-op when cfg has
	// no DatabaseDSN (CI mode).
	wipeE2EResidue(deps)

	// Layer 3: use RunUntilSignal for graceful cleanup on SIGINT/SIGTERM.
	// When CI cancels or user hits Ctrl+C, the stop function cleans up
	// all test users before the process exits.
	var exit int
	cleanupFn := func(_ context.Context) error {
		log.Println("cleaning up test users...")
		for _, u := range allUsers {
			cleanupUser(cfg, u)
		}
		// Follow the API soft-delete with a DB hard-purge so a
		// signal-interrupted run does not leak the partially
		// completed scenario's rows into the dev DB.
		wipeE2EResidue(deps)
		log.Println("cleanup done")
		return nil
	}

	runErr := runUntilSignal(func() error {
		suite := godog.TestSuite{
			Name: "crawbl-e2e",
			ScenarioInitializer: func(sc *godog.ScenarioContext) {
				initScenario(sc, cfg, users, deps)
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
	wipeE2EResidue(deps)

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
	body := &mobilev1.AuthDeleteRequest{
		Reason:      "e2e-cleanup",
		Description: "suite-level cleanup",
	}
	_, _ = tc.doProtoRequest("DELETE", "/v1/auth/delete", user.alias, body)
}

// PrintResults writes a summary to w.
func PrintResults(w io.Writer, r *Results) {
	_, _ = fmt.Fprintln(w)
	if r.Exit == 0 {
		_, _ = fmt.Fprintln(w, "All e2e tests passed.")
	} else {
		_, _ = fmt.Fprintf(w, "E2e tests failed (exit code %d).\n", r.Exit)
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

func newTestContext(cfg *Config, users *suiteUsers, deps *suiteDeps) *testContext {
	tc := &testContext{
		cfg:          cfg,
		http:         deps.http,
		dbConn:       deps.db,
		redisClient:  deps.redis,
		spacesClient: deps.spaces,
		users:        make(map[string]*testUser),
		saved:        make(map[string]string),
		state:        make(map[string]*userJourneyState),
		resolved:     make(map[string]*resolvedUser),
	}

	// All 4 users are available in every scenario.
	tc.users["primary"] = users.primary
	tc.users["frank"] = users.frank
	tc.users["grace"] = users.grace
	tc.users["zach"] = users.zach

	return tc
}

// runUntilSignal runs the supplied run function until it returns, until it
// returns an error, or until the process receives SIGINT/SIGTERM/SIGQUIT/SIGHUP.
// On signal, stop is called with a timeout context; if stop is nil the process
// exits immediately. Any signal is propagated through ctx.
func runUntilSignal(run func() error, stop func(context.Context) error, timeout time.Duration) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctx, stopNotify := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)
	defer stopNotify()

	g, _ := errgroup.WithContext(ctx)
	g.Go(func() error { return run() })

	if err := g.Wait(); err != nil {
		return err
	}

	if ctx.Err() != nil && stop != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), timeout)
		defer stopCancel()
		return stop(stopCtx)
	}

	return nil
}

func initScenario(sc *godog.ScenarioContext, cfg *Config, users *suiteUsers, deps *suiteDeps) {
	tc := newTestContext(cfg, users, deps)

	registerHTTPSteps(sc, tc)
	registerDBSteps(sc, tc)
	registerUserSteps(sc, tc)
	registerAssertionSteps(sc, tc)
	registerStateSteps(sc, tc)
	registerHealthSteps(sc, tc)
	registerAuthSteps(sc, tc)
	registerWorkspaceSteps(sc, tc)
	registerChatSteps(sc, tc)
	registerAgentSteps(sc, tc)
	registerIsolationSteps(sc, tc)
	registerIntegrationSteps(sc, tc)
	registerRedisSteps(sc, tc)
	registerSpacesSteps(sc, tc)
	registerMempalaceSteps(sc, tc)
	registerIdentitySteps(sc, tc)
	registerAuditSteps(sc, tc)
	registerRiverSteps(sc, tc)
	registerQuotaSteps(sc, tc)
	registerUsageCountersSteps(sc, tc)
	registerBlueprintSteps(sc, tc)
	registerStreamSteps(sc, tc)
}
