package dev

import (
	"context"
	"os"
	"os/exec"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/cliexec"
)

// envFileMode is the permission bits used when creating .env files.
const envFileMode = 0o644

// shellCmd runs a command with stdout/stderr forwarded to the terminal.
// It respects ctx so Ctrl+C cancels the child process.
func shellCmd(ctx context.Context, name string, args ...string) error {
	return cliexec.Run(ctx, name, args...)
}

// silentCmd runs a command and discards output.
func silentCmd(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Run()
}

// runMigrations builds and runs the migration container against the local
// PostgreSQL instance. Both 'crawbl dev migrate' and 'crawbl dev start'
// use this helper to avoid duplicating the two docker compose calls.
func runMigrations(ctx context.Context) error {
	if err := shellCmd(ctx, "docker", "compose", "--profile", "database", "--profile", "migration", "build", "migrations"); err != nil {
		return err
	}
	return shellCmd(ctx, "docker", "compose", "--profile", "database", "--profile", "migration", "run", "--rm", "migrations")
}

// ensureEnvFile creates .env from .env.example if it doesn't exist.
func ensureEnvFile() {
	if _, err := os.Stat(".env"); os.IsNotExist(err) {
		if data, err := os.ReadFile(".env.example"); err == nil {
			_ = os.WriteFile(".env", data, envFileMode)
		}
	}
}
