package redisclient

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// redis implements the Client interface using go-redis.
type redis struct {
	rc goredis.UniversalClient
}

// Default Redis connection configuration values.
const (
	// DefaultAddr is the default Redis server address.
	DefaultAddr = "localhost:6379"

	// DefaultDB is the default Redis database number.
	DefaultDB = 0

	// DefaultPingAttempts is the number of ping retries on startup.
	DefaultPingAttempts = 5

	// DefaultPingDelay is the delay between ping retries.
	DefaultPingDelay = 2 * time.Second
)

// Config holds the settings needed to connect to Redis.
type Config struct {
	// Addr is the Redis server address in host:port format.
	Addr string

	// Password is the Redis authentication password.
	Password string

	// DB is the Redis database number (0-15).
	DB int
}

// Client defines Redis operations used by the Crawbl platform.
// It provides type-safe wrappers around go-redis for caching, rate limiting,
// presence tracking, and membership queries.
type Client interface {
	// String operations — caching, token revocation, workspace status.

	// Get returns the value for key. Returns empty string and nil error if key does not exist.
	Get(ctx context.Context, key string) (string, error)

	// Set stores a key-value pair with an optional TTL. Pass 0 for no expiration.
	Set(ctx context.Context, key string, value any, ttl time.Duration) error

	// SetNX sets key only if it does not already exist. Returns true if the key was set.
	SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error)

	// Del removes one or more keys.
	Del(ctx context.Context, keys ...string) error

	// Exists reports whether a key exists.
	Exists(ctx context.Context, key string) (bool, error)

	// Expire sets a TTL on an existing key.
	Expire(ctx context.Context, key string, ttl time.Duration) error

	// Counter operations — rate limiting, usage tracking.

	// Incr atomically increments a key by 1 and returns the new value.
	Incr(ctx context.Context, key string) (int64, error)

	// IncrBy atomically increments a key by value and returns the new value.
	IncrBy(ctx context.Context, key string, value int64) (int64, error)

	// Hash operations — agent presence, integration metadata, FCM tokens.

	// HSet sets a single field in a hash.
	HSet(ctx context.Context, key, field string, value any) error

	// HGet returns the value of a hash field. Returns empty string and nil error if field does not exist.
	HGet(ctx context.Context, key, field string) (string, error)

	// HGetAll returns all fields and values in a hash.
	HGetAll(ctx context.Context, key string) (map[string]string, error)

	// HDel removes one or more fields from a hash.
	HDel(ctx context.Context, key string, fields ...string) error

	// Set operations — permissions, skill caches, notification tracking.

	// SAdd adds one or more members to a set.
	SAdd(ctx context.Context, key string, members ...string) error

	// SMembers returns all members of a set.
	SMembers(ctx context.Context, key string) ([]string, error)

	// SIsMember reports whether member belongs to the set.
	SIsMember(ctx context.Context, key string, member string) (bool, error)

	// SRem removes one or more members from a set.
	SRem(ctx context.Context, key string, members ...string) error

	// Lifecycle.

	// Ping verifies the connection to Redis.
	Ping(ctx context.Context) error

	// Close releases the underlying connection.
	Close() error
}
