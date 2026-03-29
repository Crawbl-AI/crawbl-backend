// Package setup provides the `crawbl setup` command that automates
// new-developer onboarding. Run it once after cloning the repo to verify
// your machine has everything needed to work with crawbl-backend.
package setup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

// NewSetupCommand creates the `crawbl setup` command.
func NewSetupCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Verify and prepare your machine for development",
		Long: `Verifies your development environment and prepares .env configuration.

What it does:
  1. Checks that required tools are installed (Go, Docker, kubectl, etc.)
  2. Creates .env from .env.example if it doesn't exist

After setup completes, run 'crawbl dev start' to start the orchestrator.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetup()
		},
	}

	return cmd
}

// runSetup runs the two-step setup process:
// 1. Verify all required tools are installed
// 2. Create .env if it doesn't exist
func runSetup() error {
	out.Ln()
	out.Step(style.Setup, "Crawbl Backend Setup")
	out.Ln()

	// --- Step 1: Check required tools ---
	out.Step(style.Setup, "Step 1/2: Checking required tools...")
	out.Ln()

	allFound := true
	tools := []toolCheck{
		{"go", "go version", "https://go.dev/dl/ or: mise install go"},
		{"docker", "docker --version", "https://docs.docker.com/get-docker/"},
		{"kubectl", "kubectl version --client --short 2>/dev/null || kubectl version --client", "mise install kubectl"},
		{"helm", "helm version --short", "mise install helm"},
		{"doctl", "doctl version", "mise install doctl"},
		{"aws", "aws --version", "mise install awscli"},
		{"pulumi", "pulumi version", "mise install pulumi"},
	}

	for _, t := range tools {
		if checkTool(t) {
			out.Step(style.Check, "%s", t.name)
		} else {
			out.Step(style.Failure, "%s — install: %s", t.name, t.installHint)
			allFound = false
		}
	}
	out.Ln()

	// If anything is missing, suggest mise as the fix.
	if !allFound {
		out.Step(style.Tip, "Install all tools at once with mise:")
		out.Infof("curl https://mise.run | sh")
		out.Infof("eval \"$(~/.local/bin/mise activate zsh)\"")
		out.Infof("mise install")
		out.Ln()
	}

	// --- Step 2: Check/create .env file ---
	out.Step(style.Config, "Step 2/2: Checking .env file...")
	out.Ln()

	if _, err := os.Stat(".env"); os.IsNotExist(err) {
		if _, err := os.Stat(".env.example"); err == nil {
			if err := copyFile(".env.example", ".env"); err != nil {
				return fmt.Errorf("failed to create .env: %w", err)
			}
			out.Step(style.Check, "Created .env from .env.example")
		} else {
			out.Warning("No .env.example found — create .env manually")
		}
	} else {
		out.Step(style.Check, ".env already exists")
	}
	out.Ln()

	// --- Done ---
	out.Step(style.Celebrate, "Ready! Next steps:")
	out.Infof("1. Source your environment:")
	out.Infof("   set -a && source .env && set +a")
	out.Infof("2. Start everything:")
	out.Infof("   crawbl dev start")
	out.Infof("3. Verify:")
	out.Infof("   curl http://localhost:7171/v1/health")
	out.Step(style.Doc, "Docs: https://dev.docs.crawbl.com/getting-started")
	out.Ln()

	return nil
}

// --- Helpers ---

// toolCheck holds the info needed to verify one tool is installed.
type toolCheck struct {
	name        string // Display name (e.g. "go")
	checkCmd    string // Shell command to run (e.g. "go version")
	installHint string // How to install if missing
}

// checkTool runs the check command and returns true if the tool exists.
func checkTool(t toolCheck) bool {
	parts := strings.Fields(t.checkCmd)
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

// copyFile copies src to dst.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(filepath.Clean(src))
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
