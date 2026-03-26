// Package redisclient provides a Redis client configured from environment variables.
// It follows the same config-from-env pattern used by the database package,
// and reads secrets via configenv.SecretString to support Vault Agent Injector file injection.
package redisclient

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/redis/go-redis/v9"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/configenv"
)

const (
	DefaultAddr = "localhost:6379"
	DefaultDB   = 0
)

// Config holds the settings needed to connect to Redis.
type Config struct {
	Addr     string
	Password string
	DB       int
}

// ConfigFromEnv builds a Config from environment variables using the given prefix.
// It reads:
//   - {prefix}REDIS_ADDR     (default: "localhost:6379")
//   - {prefix}REDIS_PASSWORD via configenv.SecretString, which checks {prefix}REDIS_PASSWORD
//     first, then {prefix}REDIS_PASSWORD_FILE as a file path fallback.
//   - {prefix}REDIS_DB       (default: 0)
func ConfigFromEnv(prefix string) Config {
	return Config{
		Addr:     envOrDefault(prefix+"REDIS_ADDR", DefaultAddr),
		Password: configenv.SecretString(prefix+"REDIS_PASSWORD", ""),
		DB:       intFromEnv(prefix+"REDIS_DB", DefaultDB),
	}
}

// New creates a Redis client from cfg, verifies connectivity with a Ping, and returns it.
// The caller is responsible for closing the client when done.
func New(cfg Config) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	if err := client.Ping(context.Background()).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return client, nil
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
