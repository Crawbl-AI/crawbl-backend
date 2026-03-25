package database

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/configenv"
	"github.com/gocraft/dbr/v2"
	"github.com/gocraft/dbr/v2/dialect"
	"github.com/lib/pq"
	_ "github.com/lib/pq"
)

func ConfigFromEnv(prefix string) Config {
	return Config{
		Host:               envOrDefault(prefix+"DATABASE_HOST", DefaultHost),
		Port:               envOrDefault(prefix+"DATABASE_PORT", DefaultPort),
		User:               configenv.SecretString(prefix+"DATABASE_USER", DefaultUser),
		Password:           configenv.SecretString(prefix+"DATABASE_PASSWORD", DefaultPassword),
		Name:               envOrDefault(prefix+"DATABASE_NAME", DefaultName),
		Schema:             envOrDefault(prefix+"DATABASE_SCHEMA", DefaultSchema),
		SSLMode:            envOrDefault(prefix+"DATABASE_SSLMODE", DefaultSSLMode),
		MaxOpenConnections: intFromEnv(prefix+"DATABASE_MAX_OPEN_CONNECTIONS", DefaultMaxOpenConnections),
		MaxIdleConnections: intFromEnv(prefix+"DATABASE_MAX_IDLE_CONNECTIONS", DefaultMaxIdleConnections),
		ConnMaxLifetime:    durationFromEnv(prefix+"DATABASE_CONN_MAX_LIFETIME", DefaultConnMaxLifetime),
	}
}

type SessionRunner interface {
	Select(column ...interface{}) *dbr.SelectStmt
	SelectBySql(query string, value ...interface{}) *dbr.SelectStmt
	InsertInto(table string) *dbr.InsertStmt
	InsertBySql(query string, value ...interface{}) *dbr.InsertStmt
	Update(table string) *dbr.UpdateStmt
	DeleteFrom(table string) *dbr.DeleteStmt
}

func New(config Config) (*dbr.Connection, error) {
	db, err := sql.Open("postgres", buildDriverDSN(config, true))
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(config.MaxOpenConnections)
	db.SetMaxIdleConns(config.MaxIdleConnections)
	db.SetConnMaxLifetime(config.ConnMaxLifetime)

	if err := pingWithRetry(db, DefaultPingAttempts, DefaultPingDelay); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return &dbr.Connection{
		DB:            db,
		Dialect:       dialect.PostgreSQL,
		EventReceiver: &dbr.NullEventReceiver{},
	}, nil
}

func EnsureSchema(config Config) error {
	if strings.TrimSpace(config.Schema) == "" {
		return nil
	}

	db, err := sql.Open("postgres", BuildDSN(config, false))
	if err != nil {
		return err
	}
	defer db.Close()

	if err := pingWithRetry(db, DefaultPingAttempts, DefaultPingDelay); err != nil {
		return fmt.Errorf("ping db for schema bootstrap: %w", err)
	}

	_, err = db.Exec(`CREATE SCHEMA IF NOT EXISTS "` + config.Schema + `"`)
	return err
}

func BuildDSN(config Config, includeSchema bool) string {
	dsnURL := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(config.User, config.Password),
		Host:   net.JoinHostPort(config.Host, config.Port),
		Path:   "/" + config.Name,
	}

	query := url.Values{}
	query.Set("sslmode", config.SSLMode)
	if includeSchema && strings.TrimSpace(config.Schema) != "" {
		query.Set("search_path", config.Schema)
	}
	dsnURL.RawQuery = query.Encode()

	return dsnURL.String()
}

func IsRecordExistsError(err error) bool {
	if err == nil {
		return false
	}

	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

func IsRecordNotFoundError(err error) bool {
	if err == nil {
		return false
	}

	return errors.Is(err, dbr.ErrNotFound) || errors.Is(err, sql.ErrNoRows) || err == dbr.ErrNotFound
}

func pingWithRetry(db *sql.DB, attempts int, delay time.Duration) error {
	var lastErr error

	for range attempts {
		if err := db.Ping(); err == nil {
			return nil
		} else {
			lastErr = err
		}

		time.Sleep(delay)
	}

	return lastErr
}

func buildDriverDSN(config Config, includeSchema bool) string {
	parts := []string{
		fmt.Sprintf("host=%s", config.Host),
		fmt.Sprintf("port=%s", config.Port),
		fmt.Sprintf("user=%s", config.User),
		fmt.Sprintf("password=%s", config.Password),
		fmt.Sprintf("dbname=%s", config.Name),
		fmt.Sprintf("sslmode=%s", config.SSLMode),
	}
	if includeSchema && strings.TrimSpace(config.Schema) != "" {
		parts = append(parts, fmt.Sprintf("search_path=%s", config.Schema))
	}
	return strings.Join(parts, " ")
}

func envOrDefault(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func intFromEnv(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func durationFromEnv(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}
