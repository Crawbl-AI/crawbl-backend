// Package session provides the crawbl-agent-runtime's ADK session.Service
// implementation backed by Redis.
//
// ADK's runner package expects a session.Service to create, load, append
// events to, and delete per-user sessions. The framework itself ships
// only an in-memory implementation; this package replaces that with a
// Redis-backed one so session state survives pod restarts and can be
// shared across horizontally scaled runtime pods if a workspace ever
// needs more than one replica.
//
// Key layout:
//
//	crawbl:session:{app}:{user}:{sid}       → JSON blob of session metadata + state
//	crawbl:events:{app}:{user}:{sid}        → Redis list of JSON-encoded events
//	crawbl:state:app:{app}                  → hash of app-scoped state
//	crawbl:state:user:{app}:{user}          → hash of user-scoped state
//	crawbl:sessions:{app}:{user}            → set of session IDs for List()
//
// All session keys are written with a TTL (configurable, default 24h)
// so orphaned sessions expire automatically instead of growing the
// Redis key-space forever. App- and user-scoped state hashes do NOT
// expire — they carry preferences and bookkeeping that must survive
// individual session lifetimes.
//
// Event serialization uses encoding/json directly against
// session.Event. The embedded model.LLMResponse holds *genai.Content
// values which are JSON-native (Google's Gen AI SDK defines them as
// wire-protocol types), so no custom marshaling is required.
package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	adksession "google.golang.org/adk/session"
)

// DefaultSessionTTL is the expiration applied to session keys in Redis.
// Sessions that go idle longer than this get garbage-collected
// automatically; a new Converse turn on the same session_id simply
// recreates them.
const DefaultSessionTTL = 24 * time.Hour

// maxSessionEvents caps the Redis events list per session. Each turn
// produces ~4 events (user, model, tool_call, tool_result), so 60
// events covers ~15 turns — well within the model's context window.
// Older events are trimmed on every append via LTrim.
const maxSessionEvents = 60

// RedisService implements google.golang.org/adk/session.Service against
// a Redis backend. Safe for concurrent use — every method takes its own
// Redis round-trip and does not share mutable state across calls.
type RedisService struct {
	client redis.UniversalClient
	ttl    time.Duration
}

// NewRedisService constructs a Redis-backed session service. ttl=0
// falls back to DefaultSessionTTL. The caller owns the redis.UniversalClient
// lifecycle; Close() here only releases the RedisService's reference,
// which is the same client — callers that want to keep using Redis
// after closing the session service should share the handle.
func NewRedisService(client redis.UniversalClient, ttl time.Duration) *RedisService {
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	return &RedisService{client: client, ttl: ttl}
}

// Close releases the underlying redis client.
func (s *RedisService) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}

// Create implements adksession.Service.Create.
func (s *RedisService) Create(ctx context.Context, req *adksession.CreateRequest) (*adksession.CreateResponse, error) {
	if req.AppName == "" || req.UserID == "" {
		return nil, fmt.Errorf("session/redis: app_name and user_id are required, got app_name=%q user_id=%q", req.AppName, req.UserID)
	}
	sid := req.SessionID
	if sid == "" {
		sid = uuid.NewString()
	}

	sessKey := sessionKey(req.AppName, req.UserID, sid)
	exists, err := s.client.Exists(ctx, sessKey).Result()
	if err != nil {
		return nil, fmt.Errorf("session/redis: check existence: %w", err)
	}
	if exists > 0 {
		return nil, fmt.Errorf("session/redis: session %q already exists", sid)
	}

	// Split initial state into app/user/session scopes so each scope
	// lives in its durable hash. The session-local map is persisted
	// alongside the session JSON blob.
	appDelta, userDelta, sessState := splitStateDeltas(req.State)

	if err := s.mergeAppState(ctx, req.AppName, appDelta); err != nil {
		return nil, err
	}
	if err := s.mergeUserState(ctx, req.AppName, req.UserID, userDelta); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	payload := sessionPayload{
		AppName:   req.AppName,
		UserID:    req.UserID,
		SessionID: sid,
		State:     sessState,
		UpdatedAt: now,
	}
	if err := s.writeSession(ctx, sessKey, &payload); err != nil {
		return nil, err
	}
	// Maintain the per-user session index for List(). Apply the same TTL
	// as session data so the index does not grow unboundedly.
	idxKey := userSessionsKey(req.AppName, req.UserID)
	idxPipe := s.client.TxPipeline()
	idxPipe.SAdd(ctx, idxKey, sid)
	if s.ttl > 0 {
		idxPipe.Expire(ctx, idxKey, s.ttl)
	}
	if _, err := idxPipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("session/redis: index session: %w", err)
	}

	sess, err := s.loadSession(ctx, req.AppName, req.UserID, sid, 0, time.Time{})
	if err != nil {
		return nil, err
	}
	return &adksession.CreateResponse{Session: sess}, nil
}

// Get implements adksession.Service.Get.
func (s *RedisService) Get(ctx context.Context, req *adksession.GetRequest) (*adksession.GetResponse, error) {
	if req.AppName == "" || req.UserID == "" || req.SessionID == "" {
		return nil, fmt.Errorf("session/redis: app_name, user_id, session_id are required")
	}
	sess, err := s.loadSession(ctx, req.AppName, req.UserID, req.SessionID, req.NumRecentEvents, req.After)
	if err != nil {
		return nil, err
	}
	return &adksession.GetResponse{Session: sess}, nil
}

// List implements adksession.Service.List.
func (s *RedisService) List(ctx context.Context, req *adksession.ListRequest) (*adksession.ListResponse, error) {
	if req.AppName == "" {
		return nil, fmt.Errorf("session/redis: app_name is required")
	}
	ids, err := s.client.SMembers(ctx, userSessionsKey(req.AppName, req.UserID)).Result()
	if err != nil {
		return nil, fmt.Errorf("session/redis: list session ids: %w", err)
	}
	out := make([]adksession.Session, 0, len(ids))
	for _, sid := range ids {
		sess, loadErr := s.loadSession(ctx, req.AppName, req.UserID, sid, 0, time.Time{})
		if loadErr != nil {
			// Session TTL expired between SMembers and the load —
			// clean up the dangling index entry and skip.
			if errors.Is(loadErr, errSessionNotFound) {
				_ = s.client.SRem(ctx, userSessionsKey(req.AppName, req.UserID), sid).Err()
				continue
			}
			return nil, loadErr
		}
		out = append(out, sess)
	}
	return &adksession.ListResponse{Sessions: out}, nil
}

// Delete implements adksession.Service.Delete.
func (s *RedisService) Delete(ctx context.Context, req *adksession.DeleteRequest) error {
	if req.AppName == "" || req.UserID == "" || req.SessionID == "" {
		return fmt.Errorf("session/redis: app_name, user_id, session_id are required")
	}
	pipe := s.client.TxPipeline()
	pipe.Del(ctx, sessionKey(req.AppName, req.UserID, req.SessionID))
	pipe.Del(ctx, eventsKey(req.AppName, req.UserID, req.SessionID))
	pipe.SRem(ctx, userSessionsKey(req.AppName, req.UserID), req.SessionID)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("session/redis: delete session: %w", err)
	}
	return nil
}

// AppendEvent implements adksession.Service.AppendEvent. Only persists
// finalized events — partial streaming chunks are a no-op (mirrors the
// inmemory service behavior; otherwise every SSE delta would bloat the
// events list).
func (s *RedisService) AppendEvent(ctx context.Context, cur adksession.Session, event *adksession.Event) error {
	if cur == nil {
		return fmt.Errorf("session/redis: session is nil")
	}
	if event == nil {
		return fmt.Errorf("session/redis: event is nil")
	}
	if event.Partial {
		return nil
	}

	// Strip temp-scoped state keys so they don't persist past the
	// current invocation. KeyPrefixTemp is explicitly defined in ADK
	// as "discarded after the invocation completes".
	if len(event.Actions.StateDelta) > 0 {
		filtered := make(map[string]any, len(event.Actions.StateDelta))
		for k, v := range event.Actions.StateDelta {
			if !strings.HasPrefix(k, adksession.KeyPrefixTemp) {
				filtered[k] = v
			}
		}
		event.Actions.StateDelta = filtered
	}

	rs, ok := cur.(*redisSession)
	if !ok {
		return fmt.Errorf("session/redis: unexpected session type %T", cur)
	}

	// Mutate the in-memory copy first so subsequent calls within the
	// same turn see the update.
	rs.mu.Lock()
	rs.events = append(rs.events, event)
	applyStateDelta(rs, event)
	rs.updatedAt = event.Timestamp
	if rs.updatedAt.IsZero() {
		rs.updatedAt = time.Now().UTC()
	}
	stateCopy := maps.Clone(rs.state)
	rs.mu.Unlock()

	// Split delta for durable scopes.
	appDelta, userDelta, _ := splitStateDeltas(event.Actions.StateDelta)
	if err := s.mergeAppState(ctx, rs.AppName(), appDelta); err != nil {
		return err
	}
	if err := s.mergeUserState(ctx, rs.AppName(), rs.UserID(), userDelta); err != nil {
		return err
	}

	// Persist: append event JSON to the events list and rewrite the
	// session blob with the updated session-local state and timestamp.
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("session/redis: marshal event: %w", err)
	}

	eventsK := eventsKey(rs.AppName(), rs.UserID(), rs.ID())
	sessK := sessionKey(rs.AppName(), rs.UserID(), rs.ID())

	pipe := s.client.TxPipeline()
	pipe.RPush(ctx, eventsK, eventJSON)
	pipe.LTrim(ctx, eventsK, -maxSessionEvents, -1)
	pipe.Expire(ctx, eventsK, s.ttl)

	payload := sessionPayload{
		AppName:   rs.AppName(),
		UserID:    rs.UserID(),
		SessionID: rs.ID(),
		State:     stateCopy,
		UpdatedAt: rs.updatedAt,
	}
	blob, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("session/redis: marshal session: %w", err)
	}
	pipe.Set(ctx, sessK, blob, s.ttl)

	// Refresh TTL on the session index so active conversations extend it.
	idxKey := userSessionsKey(rs.AppName(), rs.UserID())
	if s.ttl > 0 {
		pipe.Expire(ctx, idxKey, s.ttl)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("session/redis: persist event: %w", err)
	}
	return nil
}

// loadSession fetches a session + its events from Redis and returns a
// *redisSession that satisfies adksession.Session. State is merged
// across scopes (app + user + session) to match the behavior of the
// reference in-memory implementation.
func (s *RedisService) loadSession(ctx context.Context, app, user, sid string, numRecent int, after time.Time) (*redisSession, error) {
	sessK := sessionKey(app, user, sid)
	blob, err := s.client.Get(ctx, sessK).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, errSessionNotFound
		}
		return nil, fmt.Errorf("session/redis: load session: %w", err)
	}
	var payload sessionPayload
	if err := json.Unmarshal(blob, &payload); err != nil {
		return nil, fmt.Errorf("session/redis: decode session: %w", err)
	}
	// Extend TTL on touch so active sessions don't expire mid-conversation.
	if s.ttl > 0 {
		_ = s.client.Expire(ctx, sessK, s.ttl).Err()
	}

	appState, err := s.loadAppState(ctx, app)
	if err != nil {
		return nil, err
	}
	userState, err := s.loadUserState(ctx, app, user)
	if err != nil {
		return nil, err
	}

	// Merge scopes. Session-local state wins over user which wins over
	// app, matching the inmemory service merge order.
	merged := make(map[string]any, len(appState)+len(userState)+len(payload.State))
	for k, v := range appState {
		merged[adksession.KeyPrefixApp+k] = v
	}
	for k, v := range userState {
		merged[adksession.KeyPrefixUser+k] = v
	}
	for k, v := range payload.State {
		merged[k] = v
	}

	events, err := s.loadEvents(ctx, app, user, sid)
	if err != nil {
		return nil, err
	}
	events = filterEvents(events, numRecent, after)

	return &redisSession{
		appName:   payload.AppName,
		userID:    payload.UserID,
		sessionID: payload.SessionID,
		state:     merged,
		events:    events,
		updatedAt: payload.UpdatedAt,
	}, nil
}

// applyStateDelta writes session-scoped keys from a state delta into rs.state.
// App- and user-scoped keys are skipped — those are tracked in separate hashes.
// Must be called with rs.mu held.
func applyStateDelta(rs *redisSession, event *adksession.Event) {
	if len(event.Actions.StateDelta) == 0 {
		return
	}
	if rs.state == nil {
		rs.state = make(map[string]any)
	}
	for k, v := range event.Actions.StateDelta {
		switch {
		case strings.HasPrefix(k, adksession.KeyPrefixApp),
			strings.HasPrefix(k, adksession.KeyPrefixUser):
			// App- and user-scoped deltas are tracked separately.
		default:
			rs.state[k] = v
		}
	}
}

// filterEvents applies numRecent and after-timestamp filters to a slice of events.
func filterEvents(events []*adksession.Event, numRecent int, after time.Time) []*adksession.Event {
	if numRecent > 0 && len(events) > numRecent {
		events = events[len(events)-numRecent:]
	}
	if after.IsZero() {
		return events
	}
	filtered := events[:0]
	for _, e := range events {
		if !e.Timestamp.Before(after) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func (s *RedisService) loadEvents(ctx context.Context, app, user, sid string) ([]*adksession.Event, error) {
	raw, err := s.client.LRange(ctx, eventsKey(app, user, sid), 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("session/redis: load events: %w", err)
	}
	out := make([]*adksession.Event, 0, len(raw))
	for _, r := range raw {
		var ev adksession.Event
		if err := json.Unmarshal([]byte(r), &ev); err != nil {
			return nil, fmt.Errorf("session/redis: decode event: %w", err)
		}
		out = append(out, &ev)
	}
	return out, nil
}

func (s *RedisService) writeSession(ctx context.Context, key string, payload *sessionPayload) error {
	blob, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("session/redis: marshal session: %w", err)
	}
	if err := s.client.Set(ctx, key, blob, s.ttl).Err(); err != nil {
		return fmt.Errorf("session/redis: write session: %w", err)
	}
	return nil
}

func (s *RedisService) loadAppState(ctx context.Context, app string) (map[string]any, error) {
	return s.loadStateHash(ctx, appStateKey(app))
}

func (s *RedisService) loadUserState(ctx context.Context, app, user string) (map[string]any, error) {
	return s.loadStateHash(ctx, userStateKey(app, user))
}

func (s *RedisService) loadStateHash(ctx context.Context, key string) (map[string]any, error) {
	raw, err := s.client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("session/redis: load state hash %q: %w", key, err)
	}
	out := make(map[string]any, len(raw))
	for k, v := range raw {
		var decoded any
		if err := json.Unmarshal([]byte(v), &decoded); err != nil {
			// Stored value is not JSON — keep as string so the caller
			// sees a usable value instead of an error.
			out[k] = v
			continue
		}
		out[k] = decoded
	}
	return out, nil
}

func (s *RedisService) mergeAppState(ctx context.Context, app string, delta map[string]any) error {
	if len(delta) == 0 {
		return nil
	}
	// App-scoped state is permanent — no TTL. It carries preferences and
	// bookkeeping that must survive individual session lifetimes.
	return s.mergeHash(ctx, appStateKey(app), delta, false)
}

func (s *RedisService) mergeUserState(ctx context.Context, app, user string, delta map[string]any) error {
	if len(delta) == 0 {
		return nil
	}
	// User-scoped state is permanent — no TTL. Same rationale as app state.
	return s.mergeHash(ctx, userStateKey(app, user), delta, false)
}

func (s *RedisService) mergeHash(ctx context.Context, key string, delta map[string]any, withTTL bool) error {
	if len(delta) == 0 {
		return nil
	}
	fields := make(map[string]any, len(delta))
	for k, v := range delta {
		blob, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("session/redis: marshal state value for %q: %w", k, err)
		}
		fields[k] = blob
	}
	pipe := s.client.TxPipeline()
	pipe.HSet(ctx, key, fields)
	if withTTL && s.ttl > 0 {
		pipe.Expire(ctx, key, s.ttl)
	}
	_, err := pipe.Exec(ctx)
	return err
}

// splitStateDeltas partitions a state delta map by scope prefix. The
// app and user scopes get their prefix stripped so they slot directly
// into their destination Redis hash; the session-local scope keeps its
// raw key (including any non-reserved prefix) as-is.
func splitStateDeltas(delta map[string]any) (appDelta, userDelta, sessionDelta map[string]any) {
	appDelta = map[string]any{}
	userDelta = map[string]any{}
	sessionDelta = map[string]any{}
	for k, v := range delta {
		switch {
		case strings.HasPrefix(k, adksession.KeyPrefixTemp):
			// Temp scope is discarded before persistence elsewhere.
		case strings.HasPrefix(k, adksession.KeyPrefixApp):
			appDelta[strings.TrimPrefix(k, adksession.KeyPrefixApp)] = v
		case strings.HasPrefix(k, adksession.KeyPrefixUser):
			userDelta[strings.TrimPrefix(k, adksession.KeyPrefixUser)] = v
		default:
			sessionDelta[k] = v
		}
	}
	return appDelta, userDelta, sessionDelta
}

// Key helpers ---------------------------------------------------------

const keyPrefix = "crawbl"

func sessionKey(app, user, sid string) string {
	return fmt.Sprintf("%s:session:%s:%s:%s", keyPrefix, app, user, sid)
}

func eventsKey(app, user, sid string) string {
	return fmt.Sprintf("%s:events:%s:%s:%s", keyPrefix, app, user, sid)
}

func userSessionsKey(app, user string) string {
	return fmt.Sprintf("%s:sessions:%s:%s", keyPrefix, app, user)
}

func appStateKey(app string) string {
	return fmt.Sprintf("%s:state:app:%s", keyPrefix, app)
}

func userStateKey(app, user string) string {
	return fmt.Sprintf("%s:state:user:%s:%s", keyPrefix, app, user)
}

// errSessionNotFound is returned when Get hits a missing key. Kept
// private because callers map it to a service-specific error at the
// boundary.
var errSessionNotFound = errors.New("session/redis: session not found")

// sessionPayload is the JSON shape persisted under the session key.
// Events live in a separate Redis list so the main payload stays
// small and doesn't get rewritten on every AppendEvent.
type sessionPayload struct {
	AppName   string         `json:"app_name"`
	UserID    string         `json:"user_id"`
	SessionID string         `json:"session_id"`
	State     map[string]any `json:"state"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// redisSession is the concrete adksession.Session returned by
// Create/Get/List. It carries a snapshot of the session's state and
// events at the time of the load; AppendEvent mutates both the
// in-memory copy (for this turn's downstream access) and Redis (for
// persistence).
type redisSession struct {
	appName   string
	userID    string
	sessionID string

	mu        sync.RWMutex
	events    []*adksession.Event
	state     map[string]any
	updatedAt time.Time
}

func (s *redisSession) ID() string      { return s.sessionID }
func (s *redisSession) AppName() string { return s.appName }
func (s *redisSession) UserID() string  { return s.userID }
func (s *redisSession) LastUpdateTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.updatedAt
}
func (s *redisSession) State() adksession.State   { return &redisState{owner: s} }
func (s *redisSession) Events() adksession.Events { return &redisEvents{owner: s} }

// redisState satisfies adksession.State against the session's in-memory
// map. Mutations via Set() update the map in place; durable persistence
// happens through AppendEvent's state delta pipeline.
type redisState struct{ owner *redisSession }

func (s *redisState) Get(k string) (any, error) {
	s.owner.mu.RLock()
	defer s.owner.mu.RUnlock()
	v, ok := s.owner.state[k]
	if !ok {
		return nil, adksession.ErrStateKeyNotExist
	}
	return v, nil
}

func (s *redisState) Set(k string, v any) error {
	s.owner.mu.Lock()
	defer s.owner.mu.Unlock()
	if s.owner.state == nil {
		s.owner.state = make(map[string]any)
	}
	s.owner.state[k] = v
	return nil
}

func (s *redisState) All() iter.Seq2[string, any] {
	s.owner.mu.RLock()
	clone := maps.Clone(s.owner.state)
	s.owner.mu.RUnlock()
	return func(yield func(string, any) bool) {
		for k, v := range clone {
			if !yield(k, v) {
				return
			}
		}
	}
}

// redisEvents satisfies adksession.Events over the session's in-memory
// slice. The slice is cloned on iteration to keep concurrent writes
// safe.
type redisEvents struct{ owner *redisSession }

func (e *redisEvents) All() iter.Seq[*adksession.Event] {
	e.owner.mu.RLock()
	clone := slices.Clone(e.owner.events)
	e.owner.mu.RUnlock()
	return func(yield func(*adksession.Event) bool) {
		for _, ev := range clone {
			if !yield(ev) {
				return
			}
		}
	}
}

func (e *redisEvents) Len() int {
	e.owner.mu.RLock()
	defer e.owner.mu.RUnlock()
	return len(e.owner.events)
}

func (e *redisEvents) At(i int) *adksession.Event {
	e.owner.mu.RLock()
	defer e.owner.mu.RUnlock()
	if i < 0 || i >= len(e.owner.events) {
		return nil
	}
	return e.owner.events[i]
}

// Compile-time assertions.
var (
	_ adksession.Service = (*RedisService)(nil)
	_ adksession.Session = (*redisSession)(nil)
	_ adksession.State   = (*redisState)(nil)
	_ adksession.Events  = (*redisEvents)(nil)
)
