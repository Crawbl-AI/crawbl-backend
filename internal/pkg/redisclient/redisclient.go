// Package redisclient provides a Redis client configured from environment variables.
// It wraps go-redis behind a clean Client interface for testability and provides
// operations needed by the Crawbl platform: caching, counters, hashes, and sets.
package redisclient

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/configenv"
)

// Ensure redis implements Client at compile time.
var _ Client = (*redis)(nil)

// ConfigFromEnv builds a Config from environment variables using the given prefix.
// It reads:
//   - {prefix}REDIS_ADDR     (default: "localhost:6379")
//   - {prefix}REDIS_PASSWORD  via configenv.SecretString (supports _FILE fallback)
//   - {prefix}REDIS_DB        (default: 0)
func ConfigFromEnv(prefix string) Config {
	return Config{
		Addr:     envOrDefault(prefix+"REDIS_ADDR", DefaultAddr),
		Password: configenv.SecretString(prefix+"REDIS_PASSWORD", ""),
		DB:       intFromEnv(prefix+"REDIS_DB", DefaultDB),
	}
}

// New creates a Redis client from cfg, verifies connectivity with retry logic,
// and returns a Client. The caller is responsible for calling Close when done.
func New(cfg Config) (Client, error) {
	rc := goredis.NewClient(&goredis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	if err := pingWithRetry(context.Background(), rc, DefaultPingAttempts, DefaultPingDelay); err != nil {
		_ = rc.Close()
		return nil, fmt.Errorf("connect redis: %w", err)
	}

	return &redis{rc: rc}, nil
}

// Unwrap returns the underlying go-redis client for advanced usage
// such as the Socket.IO Redis adapter. Use sparingly.
func Unwrap(c Client) *goredis.Client {
	r, ok := c.(*redis)
	if !ok {
		return nil
	}
	return r.rc
}

// --- String operations ---

func (r *redis) Get(ctx context.Context, key string) (string, error) {
	val, err := r.rc.Get(ctx, key).Result()
	if err == goredis.Nil {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("redis get %q: %w", key, err)
	}
	return val, nil
}

func (r *redis) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if err := r.rc.Set(ctx, key, value, ttl).Err(); err != nil {
		return fmt.Errorf("redis set %q: %w", key, err)
	}
	return nil
}

func (r *redis) SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	ok, err := r.rc.SetArgs(ctx, key, value, goredis.SetArgs{
		Mode: "NX",
		TTL:  ttl,
	}).Result()
	if err != nil && err != goredis.Nil {
		return false, fmt.Errorf("redis setnx %q: %w", key, err)
	}
	return ok == "OK", nil
}

func (r *redis) Del(ctx context.Context, keys ...string) error {
	if err := r.rc.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("redis del: %w", err)
	}
	return nil
}

func (r *redis) Exists(ctx context.Context, key string) (bool, error) {
	n, err := r.rc.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("redis exists %q: %w", key, err)
	}
	return n > 0, nil
}

func (r *redis) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if err := r.rc.Expire(ctx, key, ttl).Err(); err != nil {
		return fmt.Errorf("redis expire %q: %w", key, err)
	}
	return nil
}

// --- Counter operations ---

func (r *redis) Incr(ctx context.Context, key string) (int64, error) {
	val, err := r.rc.Incr(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("redis incr %q: %w", key, err)
	}
	return val, nil
}

func (r *redis) IncrBy(ctx context.Context, key string, value int64) (int64, error) {
	val, err := r.rc.IncrBy(ctx, key, value).Result()
	if err != nil {
		return 0, fmt.Errorf("redis incrby %q: %w", key, err)
	}
	return val, nil
}

// --- Hash operations ---

func (r *redis) HSet(ctx context.Context, key, field string, value any) error {
	if err := r.rc.HSet(ctx, key, field, value).Err(); err != nil {
		return fmt.Errorf("redis hset %q %q: %w", key, field, err)
	}
	return nil
}

func (r *redis) HGet(ctx context.Context, key, field string) (string, error) {
	val, err := r.rc.HGet(ctx, key, field).Result()
	if err == goredis.Nil {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("redis hget %q %q: %w", key, field, err)
	}
	return val, nil
}

func (r *redis) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	val, err := r.rc.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("redis hgetall %q: %w", key, err)
	}
	return val, nil
}

func (r *redis) HDel(ctx context.Context, key string, fields ...string) error {
	if err := r.rc.HDel(ctx, key, fields...).Err(); err != nil {
		return fmt.Errorf("redis hdel %q: %w", key, err)
	}
	return nil
}

// --- Set operations ---

func (r *redis) SAdd(ctx context.Context, key string, members ...string) error {
	args := make([]any, len(members))
	for i, m := range members {
		args[i] = m
	}
	if err := r.rc.SAdd(ctx, key, args...).Err(); err != nil {
		return fmt.Errorf("redis sadd %q: %w", key, err)
	}
	return nil
}

func (r *redis) SMembers(ctx context.Context, key string) ([]string, error) {
	val, err := r.rc.SMembers(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("redis smembers %q: %w", key, err)
	}
	return val, nil
}

func (r *redis) SIsMember(ctx context.Context, key string, member string) (bool, error) {
	ok, err := r.rc.SIsMember(ctx, key, member).Result()
	if err != nil {
		return false, fmt.Errorf("redis sismember %q %q: %w", key, member, err)
	}
	return ok, nil
}

func (r *redis) SRem(ctx context.Context, key string, members ...string) error {
	args := make([]any, len(members))
	for i, m := range members {
		args[i] = m
	}
	if err := r.rc.SRem(ctx, key, args...).Err(); err != nil {
		return fmt.Errorf("redis srem %q: %w", key, err)
	}
	return nil
}

// --- Lifecycle ---

func (r *redis) Ping(ctx context.Context) error {
	if err := r.rc.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis ping: %w", err)
	}
	return nil
}

func (r *redis) Close() error {
	return r.rc.Close()
}

// --- Internal helpers ---

// pingWithRetry attempts to ping Redis with retry logic, matching the database
// package pattern for resilience during container startup.
// TODO: IS SLEEP HERE A CORRECT APPROACH ?? VERIFY THIS HIGH PRIORITY
func pingWithRetry(ctx context.Context, rc *goredis.Client, attempts int, delay time.Duration) error {
	var lastErr error

	for range attempts {
		if err := rc.Ping(ctx).Err(); err == nil {
			return nil
		} else {
			lastErr = err
		}

		time.Sleep(delay)
	}

	return lastErr
}

// envOrDefault returns the value of the environment variable key,
// or fallback if the variable is unset or empty.
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// intFromEnv parses an integer environment variable, returning fallback on missing or parse error.
func intFromEnv(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return parsed
}
