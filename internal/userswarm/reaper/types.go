package reaper

import "time"

// Config holds the configuration for a reaper run.
type Config struct {
	DatabaseDSN string
	MaxAge      time.Duration
	DryRun      bool
}

// staleUser holds the minimal fields needed to clean up a test user.
type staleUser struct {
	ID        string
	Subject   string
	Email     string
	CreatedAt time.Time
}

// Result holds the outcome of a reaper run.
type Result struct {
	UsersFound   int
	UsersReaped  int
	SwarmsReaped int
	Errors       int
}
