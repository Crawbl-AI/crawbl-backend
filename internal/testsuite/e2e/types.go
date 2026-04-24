package e2e

import (
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gocraft/dbr/v2"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/encoding/protojson"
)

// Consts

const (
	// pathWorkspaces is the base path for workspace API endpoints.
	pathWorkspaces        = "/v1/workspaces/"
	pathConversations     = "/conversations/"
	pathAgents            = "/v1/agents/"
	pathMessages          = "/messages"
	pathMemories          = "/memories"
	errDBQueryFailed      = "DB query failed: %w"
	errNoConversation     = "no current conversation set for %q"
	whereWorkspaceIDIn    = "workspace_id IN ?"
	testUserBerlinBuilder = "berlin-builder"
	jsonPathFirstID       = "data.0.id"
)

const (
	// maxBodyDisplayLen is the maximum number of characters shown when
	// truncating a response body in error messages.
	maxBodyDisplayLen = 200

	// asyncAssertTimeout is how long polling assertions wait for
	// async agent-side effects (memory, audit, delegation, etc.).
	asyncAssertTimeout = 30 * time.Second
)

// pollInterval is the fixed polling interval used by pollUntil.
const pollInterval = 1 * time.Second

const (
	// assistantReplyPollWindow bounds how long sendMessage waits for
	// the assistant's first reply to surface in the conversation's
	// messages list after the POST returns 201. 3 minutes covers the
	// longest warm-runtime reply budget plus ADK tool-call latency;
	// cold-start is already gated by the warmup step.
	assistantReplyPollWindow = 3 * time.Minute

	// assistantReplyPollInterval is how often sendMessage re-checks
	// the conversation's messages list while waiting for a reply.
	// 1 second balances responsiveness against needless load.
	assistantReplyPollInterval = 1 * time.Second
)

const (
	defaultRuntimeReadyTimeout = 3 * time.Minute
	defaultRuntimePollInterval = 2 * time.Second
)

// dbMaxOpenConns is the connection pool ceiling for the suite-scoped
// Postgres handle. 8 is enough for the parallel step goroutines that
// fan out during a single godog scenario without exhausting the
// cluster's pg_hba connection limit.
const (
	dbMaxOpenConns    = 8
	dbMaxIdleConns    = 4
	dbConnMaxLifetime = 10 * time.Minute
	redisPingTimeout  = 3 * time.Second
)

// e2eSubjectPattern is the LIKE pattern every test user's subject
// matches. It is set once in Run() from the suite's fixed
// `e2e-<alias>-<unix-ns>` subject format.
const e2eSubjectPattern = "e2e-%"

// Vars

// memoryWorkspaceTables is the set of memory_* tables that store a
// workspace_id without an ON DELETE CASCADE foreign key, so they
// must be wiped explicitly before the user delete runs.
var memoryWorkspaceTables = []string{
	"memory_drawers",
	"memory_entities",
	"memory_identities",
	"memory_triples",
}

var protoMarshaler = protojson.MarshalOptions{UseProtoNames: true}

// Types

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

// suiteDeps holds every expensive/stateful resource the e2e suite
// needs. Opened exactly once at Run() entry, closed exactly once on
// Run() exit. Previously these resources were opened per scenario
// in newTestContext which produced intermittent "sql: database is
// closed" errors when the driver's internal pool drained.
type suiteDeps struct {
	http   *http.Client
	db     *dbr.Connection
	redis  *redis.Client
	spaces *s3.Client
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

// resolvedUser is the cached result of a single subject lookup.
// Scenario steps use it instead of rolling their own JOIN query
// against users + workspaces every time.
type resolvedUser struct {
	Alias       string
	Subject     string
	UserID      string
	WorkspaceID string
	TestUser    *testUser
}

// runtimeSnapshot holds a point-in-time view of the agent runtime
// status fields returned by the GET /workspaces/:id endpoint.
type runtimeSnapshot struct {
	Status    string
	Phase     string
	Verified  bool
	LastError string
}

// blueprintState holds per-scenario original values for teardown.
type blueprintState struct {
	agentID       string
	originalModel string
	originalTools []string
}

// baselineEntry pairs an alias + agent slug with a captured token baseline.
type baselineEntry struct {
	alias    string
	slug     string
	baseline int64
}

// blueprintScenarioState is set once per scenario and cleared in AfterScenario.
var _ = (*blueprintState)(nil) // compile-time nil-pointer check

// strings package keep-alive for quotaClearForAlias in steps_usage_quota.go.
var _ = strings.TrimSpace
