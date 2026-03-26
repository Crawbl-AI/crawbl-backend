// Package build provides shared utilities for build subcommands.
package build

import (
	"fmt"
	"os/exec"
	"strings"
)

// getRootDir returns the git repository root directory.
func getRootDir() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get git root: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}
