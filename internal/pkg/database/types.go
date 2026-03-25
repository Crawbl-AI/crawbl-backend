package database

import "time"

const (
	DefaultHost               = "127.0.0.1"
	DefaultPort               = "5432"
	DefaultUser               = "postgres"
	DefaultPassword           = "postgres"
	DefaultName               = "crawbl"
	DefaultSchema             = "orchestrator"
	DefaultSSLMode            = "disable"
	DefaultMaxOpenConnections = 20
	DefaultMaxIdleConnections = 10
	DefaultConnMaxLifetime    = 5 * time.Minute
	DefaultPingAttempts       = 5
	DefaultPingDelay          = 2 * time.Second
)

type Config struct {
	Host               string
	Port               string
	User               string
	Password           string
	Name               string
	Schema             string
	SSLMode            string
	MaxOpenConnections int
	MaxIdleConnections int
	ConnMaxLifetime    time.Duration
}
