// Package redisclient provides a Redis client configured from environment variables.
// It wraps go-redis behind a clean Client interface for testability and provides
// operations needed by the Crawbl platform: caching, counters, hashes, and sets.
package redisclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/config"
)

// ConfigFromEnv builds a Config from environment variables using the given prefix.
// It reads:
//   - {prefix}REDIS_ADDR     (default: "localhost:6379")
//   - {prefix}REDIS_PASSWORD  via config.SecretString (supports _FILE fallback)
//   - {prefix}REDIS_DB        (default: 0)
func ConfigFromEnv(prefix string) Config {
	return Config{
		Addr:     config.StringOr(prefix+"REDIS_ADDR", DefaultAddr),
		Password: config.SecretString(prefix+"REDIS_PASSWORD", ""),
		DB:       config.IntOr(prefix+"REDIS_DB", DefaultDB),
		TLS:      config.BoolOr(prefix+"REDIS_TLS", false),
	}
}

// New creates a Redis client from cfg, verifies connectivity with retry logic,
// and returns a Client. The caller is responsible for calling Close when done.
// NewUniversalClient is used so the client is compatible with both standalone
// Redis/Valkey and cluster endpoints (e.g. DO Managed Valkey).
func New(cfg Config) (Client, error) {
	opts := &goredis.UniversalOptions{
		Addrs:        []string{cfg.Addr},
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     10,
		MinIdleConns: 3,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolTimeout:  4 * time.Second,
	}
	if cfg.TLS {
		opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	rc := goredis.NewUniversalClient(opts)

	if err := pingWithRetry(context.Background(), rc, DefaultPingAttempts, DefaultPingDelay); err != nil {
		_ = rc.Close()
		return nil, fmt.Errorf("connect redis: %w", err)
	}

	return &redis{rc: rc}, nil
}

// Unwrap returns the underlying go-redis client for advanced usage
// such as the Socket.IO Redis adapter. Use sparingly.
func Unwrap(c Client) goredis.UniversalClient {
	r, ok := c.(*redis)
	if !ok {
		return nil
	}
	return r.rc
}

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

func (r *redis) Ping(ctx context.Context) error {
	if err := r.rc.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis ping: %w", err)
	}
	return nil
}

func (r *redis) Close() error {
	return r.rc.Close()
}

// pingWithRetry attempts to ping Redis with retry logic, matching the database
// package pattern for resilience during container startup.
//
// Returns:
//   - nil on successful ping.
//   - The last error encountered if all attempts fail.
//   - ctx.Err() if the context is cancelled during a retry delay.
func pingWithRetry(ctx context.Context, rc goredis.UniversalClient, attempts int, delay time.Duration) error {
	var lastErr error

	for range attempts {
		if err := rc.Ping(ctx).Err(); err == nil {
			return nil
		} else {
			lastErr = err
		}

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return lastErr
}
