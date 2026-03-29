package dev

import (
	"os"
	"os/exec"
)

// shellCmd runs a command with stdout/stderr forwarded to the terminal.
func shellCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// silentCmd runs a command and discards output.
func silentCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	return cmd.Run()
}

// ensureEnvFile creates .env from .env.example if it doesn't exist.
func ensureEnvFile() {
	if _, err := os.Stat(".env"); os.IsNotExist(err) {
		if data, err := os.ReadFile(".env.example"); err == nil {
			_ = os.WriteFile(".env", data, 0o644)
		}
	}
}
