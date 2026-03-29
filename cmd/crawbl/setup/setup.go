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
)

// NewSetupCommand creates the `crawbl setup` command.
func NewSetupCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Check your machine is ready to work with crawbl-backend",
		Long: `Verifies your development environment and prepares .env configuration.

What it does:
  1. Checks that required tools are installed (Go, Docker, kubectl, etc.)
  2. Creates .env from .env.example if it doesn't exist

After setup completes, run 'make run' to start the orchestrator.`,
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
	fmt.Println()
	fmt.Println("🧠 Crawbl Backend Setup")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	// --- Step 1: Check required tools ---
	fmt.Println("📋 Step 1/2 — Checking required tools...")
	fmt.Println()

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
			fmt.Printf("   ✅ %s\n", t.name)
		} else {
			fmt.Printf("   ❌ %s — install: %s\n", t.name, t.installHint)
			allFound = false
		}
	}
	fmt.Println()

	// If anything is missing, suggest mise as the fix.
	if !allFound {
		fmt.Println("💡 Install all tools at once with mise:")
		fmt.Println()
		fmt.Println("   curl https://mise.run | sh")
		fmt.Println("   eval \"$(~/.local/bin/mise activate zsh)\"")
		fmt.Println("   mise install")
		fmt.Println()
	}

	// --- Step 2: Check/create .env file ---
	fmt.Println("📝 Step 2/2 — Checking .env file...")
	fmt.Println()

	if _, err := os.Stat(".env"); os.IsNotExist(err) {
		if _, err := os.Stat(".env.example"); err == nil {
			if err := copyFile(".env.example", ".env"); err != nil {
				return fmt.Errorf("failed to create .env: %w", err)
			}
			fmt.Println("   ✅ Created .env from .env.example")
		} else {
			fmt.Println("   ⚠️  No .env.example found — create .env manually")
		}
	} else {
		fmt.Println("   ✅ .env already exists")
	}
	fmt.Println()

	// --- Done ---
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("🎉 Ready! Next steps:")
	fmt.Println()
	fmt.Println("   1. Source your environment:")
	fmt.Println("      set -a && source .env && set +a")
	fmt.Println()
	fmt.Println("   2. Start everything:")
	fmt.Println("      make run")
	fmt.Println()
	fmt.Println("   3. Verify:")
	fmt.Println("      curl http://localhost:7171/v1/health")
	fmt.Println()
	fmt.Println("   📚 Docs: https://dev.docs.crawbl.com/getting-started")
	fmt.Println()

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
