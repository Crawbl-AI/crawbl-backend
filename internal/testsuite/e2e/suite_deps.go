package e2e

import (
	"context"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gocraft/dbr/v2"
	"github.com/gocraft/dbr/v2/dialect"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
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

func newSuiteDeps(cfg *Config) *suiteDeps {
	deps := &suiteDeps{
		http: &http.Client{Timeout: cfg.Timeout},
	}
	if cfg.DatabaseDSN != "" {
		if conn, err := dbr.Open("postgres", cfg.DatabaseDSN, nil); err == nil {
			conn.Dialect = dialect.PostgreSQL
			conn.SetMaxOpenConns(dbMaxOpenConns)
			conn.SetMaxIdleConns(dbMaxIdleConns)
			conn.SetConnMaxLifetime(dbConnMaxLifetime)
			deps.db = conn
		}
	}
	if cfg.RedisAddr != "" {
		client := redis.NewClient(&redis.Options{
			Addr:     cfg.RedisAddr,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		})
		pingCtx, cancel := context.WithTimeout(context.Background(), redisPingTimeout)
		if err := client.Ping(pingCtx).Err(); err == nil {
			deps.redis = client
		} else {
			_ = client.Close()
		}
		cancel()
	}
	if cfg.SpacesEndpoint != "" && cfg.SpacesBucket != "" && cfg.SpacesAccessKey != "" && cfg.SpacesSecretKey != "" {
		region := cfg.SpacesRegion
		if region == "" {
			region = "us-east-1"
		}
		deps.spaces = s3.New(s3.Options{
			Region:       region,
			Credentials:  credentials.NewStaticCredentialsProvider(cfg.SpacesAccessKey, cfg.SpacesSecretKey, ""),
			BaseEndpoint: aws.String(cfg.SpacesEndpoint),
			UsePathStyle: false,
		})
	}
	return deps
}

func (d *suiteDeps) close() {
	if d == nil {
		return
	}
	if d.db != nil {
		_ = d.db.Close()
	}
	if d.redis != nil {
		_ = d.redis.Close()
	}
	// s3.Client has no Close method.
}
