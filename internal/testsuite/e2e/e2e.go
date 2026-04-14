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
	"path/filepath"
	"runtime"
	"strings"
	"time"

	mobilev1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/mobile/v1"
	backendruntime "github.com/Crawbl-AI/crawbl-backend/internal/pkg/runtime"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	"github.com/gocraft/dbr/v2"
	"github.com/redis/go-redis/v9"
)

// Config holds the configuration for an e2e test run.
//
// The CI invocation only needs BaseURL (and optionally E2EToken for
// the gateway bypass). Everything else is optional and sourced from
// environment variables by the CLI wrapper — scenarios that need a
// specific client (Postgres, Redis, DO Spaces) skip gracefully when
// the corresponding config is absent. This keeps `crawbl test e2e
// --base-url ...` working in CI without the workflow having to know
// which infrastructure dependencies a particular scenario touches.
type Config struct {
	BaseURL             string
	E2EToken            string
	Verbose             bool
	Timeout             time.Duration
	RuntimeReadyTimeout time.Duration
	RuntimePollInterval time.Duration
	DatabaseDSN         string

	// Redis config — enables "the assistant should remember the
	// current conversation context" style assertions. When empty,
	// all Redis-backed steps are silent no-ops.
	RedisAddr     string
	RedisPassword string
	RedisDB       int

	// DO Spaces config — enables "the file should be saved in the
	// workspace file store" style assertions. When empty, all
	// Spaces-backed steps are silent no-ops.
	SpacesEndpoint  string
	SpacesRegion    string
	SpacesBucket    string
	SpacesAccessKey string
	SpacesSecretKey string

	// Category restricts the test run to a single subfolder under
	// test-features/ (e.g. "chat", "tools", "auth"). When empty,
	// all subfolders are included.
	Category string

	// Tags is a godog tag filter expression passed straight through to
	// godog.Options.Tags. Use it to skip flaky scenarios during gating
	// runs (e.g. "~@llm-flaky") or to include only specific tags
	// ("@smoke"). When empty, every scenario runs regardless of tags.
	//
	// Syntax is the standard Cucumber tag expression grammar:
	//   "@foo"              – only scenarios tagged @foo
	//   "~@bar"             – exclude scenarios tagged @bar
	//   "@foo && ~@bar"     – tagged @foo AND not @bar
	//   "@foo || @baz"      – tagged @foo OR @baz
	Tags string
}

// Results holds the aggregate outcome of a test run.
type Results struct {
	Exit int
}

// suiteUsers holds the fixed test users created once per suite run.
type suiteUsers struct {
	primary *testUser
	frank   *testUser
	grace   *testUser
	zach    *testUser
}

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

	runErr := backendruntime.RunUntilSignal(func() error {
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

// testContext holds per-scenario state shared across step definitions.
type testContext struct {
	cfg    *Config
	http   *http.Client
	dbConn *dbr.Connection
	// redisClient is set only when cfg.RedisAddr is non-empty.
	// Steps that need Redis check for nil and no-op gracefully so
	// local runs without a Redis port-forward stay green.
	redisClient *redis.Client
	// spacesClient is set only when the full Spaces config quartet
	// is present. Steps that need Spaces check for nil the same
	// way as Redis.
	spacesClient *s3.Client
	users        map[string]*testUser
	saved        map[string]string
	state        map[string]*userJourneyState
	// resolved caches subject→user→workspace lookups keyed by alias.
	// Populated lazily by resolveUser; invalidated by invalidateResolvedUser.
	resolved map[string]*resolvedUser
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
