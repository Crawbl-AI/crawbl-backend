package dev

import (
	"context"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/cliexec"
)

// shellCmd runs a command with stdout/stderr forwarded to the terminal.
func shellCmd(ctx context.Context, name string, args ...string) error {
	return cliexec.Run(ctx, name, args...)
}
