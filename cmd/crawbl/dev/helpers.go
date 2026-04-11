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

// ensureEnvFile creates .env from .env.example if it doesn't exist.
func ensureEnvFile() {
	if _, err := os.Stat(".env"); os.IsNotExist(err) {
		if data, err := os.ReadFile(".env.example"); err == nil {
			_ = os.WriteFile(".env", data, envFileMode)
		}
	}
}
