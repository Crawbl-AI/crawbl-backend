// Package cliexec provides a ctx-aware subprocess runner used by the crawbl
// CLI commands. It forwards stdin/stdout/stderr to the terminal and respects
// cancellation through the supplied context so Ctrl+C reaches the child.
package cliexec

import (
	"context"
	"io"
	"os"
	"os/exec"
)

// Run executes name with args using exec.CommandContext. Stdin/stdout/stderr
// are wired directly to the parent process so the child's output is visible
// to the user and interactive prompts work. Cancelling ctx kills the child.
func Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Silent executes name with args using exec.CommandContext and discards
// stdout/stderr. Use for commands whose output is noise and whose exit
// code is the only signal you care about. Stdin is still wired to the
// parent process so interactive prompts continue to work.
func Silent(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}
