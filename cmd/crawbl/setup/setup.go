// Package setup provides the `crawbl setup` command that automates
// new-developer onboarding. Run it once after cloning the repo to get
// a fully working local environment.
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
	var skipDocker bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set up your local development environment",
		Long: `Prepares everything you need to work with crawbl-backend.

What it does:
  1. Checks that required tools are installed (Go, Docker, kubectl, etc.)
  2. Creates .env from .env.example if it doesn't exist
  3. Starts PostgreSQL via Docker Compose
  4. Runs database migrations
  5. Verifies everything is working

After setup completes, run 'make run' to start the orchestrator.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetup(skipDocker)
		},
	}

	cmd.Flags().BoolVar(&skipDocker, "skip-docker", false, "Skip Docker/database setup (tools check only)")

	return cmd
}

func runSetup(skipDocker bool) error {
	fmt.Println()
	fmt.Println("🧠 Crawbl Backend Setup")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	// Step 1: Check required tools.
	fmt.Println("📋 Step 1/5 — Checking required tools...")
	fmt.Println()
	allFound := true
	tools := []toolCheck{
		{"go", "go version", "https://go.dev/dl/ or: mise install go"},
		{"docker", "docker --version", "https://docs.docker.com/get-docker/"},
		{"kubectl", "kubectl version --client --short 2>/dev/null || kubectl version --client", "https://kubernetes.io/docs/tasks/tools/ or: mise install kubectl"},
		{"helm", "helm version --short", "https://helm.sh/docs/intro/install/ or: mise install helm"},
		{"doctl", "doctl version", "https://docs.digitalocean.com/reference/doctl/how-to/install/ or: mise install doctl"},
		{"aws", "aws --version", "https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html or: mise install awscli"},
		{"pulumi", "pulumi version", "https://www.pulumi.com/docs/install/ or: mise install pulumi"},
	}

	for _, t := range tools {
		if checkTool(t) {
			fmt.Printf("   ✅ %s\n", t.name)
		} else {
			fmt.Printf("   ❌ %s — install from: %s\n", t.name, t.installHint)
			allFound = false
		}
	}
	fmt.Println()

	if !allFound {
		fmt.Println("💡 Tip: Install mise to manage all tool versions automatically:")
		fmt.Println("   curl https://mise.run | sh")
		fmt.Println("   eval \"$(~/.local/bin/mise activate zsh)\"")
		fmt.Println("   mise install")
		fmt.Println()
		fmt.Println("   This reads .mise.toml and installs everything at the right version.")
		fmt.Println()
	}

	// Step 2: Check/create .env file.
	fmt.Println("📝 Step 2/5 — Checking .env file...")
	fmt.Println()
	if _, err := os.Stat(".env"); os.IsNotExist(err) {
		if _, err := os.Stat(".env.example"); err == nil {
			if err := copyFile(".env.example", ".env"); err != nil {
				return fmt.Errorf("failed to create .env: %w", err)
			}
			fmt.Println("   ✅ Created .env from .env.example")
		} else {
			fmt.Println("   ⚠️  No .env.example found — you'll need to create .env manually")
		}
	} else {
		fmt.Println("   ✅ .env already exists")
	}
	fmt.Println()

	if skipDocker {
		fmt.Println("⏭️  Skipping Docker/database setup (--skip-docker)")
		fmt.Println()
		printNextSteps()
		return nil
	}

	// Step 3: Start Postgres.
	fmt.Println("🐘 Step 3/5 — Starting PostgreSQL...")
	fmt.Println()
	if !commandExists("docker") {
		fmt.Println("   ⚠️  Docker not found — skipping database setup")
		fmt.Println("   Install Docker and re-run 'crawbl setup'")
		fmt.Println()
		printNextSteps()
		return nil
	}

	if err := runCmd("docker", "compose", "--profile", "database", "up", "-d"); err != nil {
		return fmt.Errorf("failed to start Postgres: %w", err)
	}
	fmt.Println("   ✅ PostgreSQL started")
	fmt.Println()

	// Step 4: Wait for Postgres and run migrations.
	fmt.Println("🔄 Step 4/5 — Running database migrations...")
	fmt.Println()

	// Wait for Postgres to be ready (up to 30 seconds).
	ready := false
	for i := 0; i < 30; i++ {
		if err := runCmdSilent("docker", "compose", "exec", "-T", "postgresdb", "pg_isready", "-h", "postgresdb"); err == nil {
			ready = true
			break
		}
		fmt.Print(".")
	}
	if !ready {
		return fmt.Errorf("PostgreSQL did not become ready in 30 seconds")
	}
	fmt.Println()

	// Build and run migrations.
	if err := runCmd("docker", "compose", "--profile", "database", "--profile", "migration", "build", "migrations"); err != nil {
		return fmt.Errorf("failed to build migration image: %w", err)
	}
	if err := runCmd("docker", "compose", "--profile", "database", "--profile", "migration", "run", "--rm", "migrations"); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}
	fmt.Println("   ✅ Migrations applied")
	fmt.Println()

	// Step 5: Verify.
	fmt.Println("✅ Step 5/5 — Verifying setup...")
	fmt.Println()
	fmt.Println("   ✅ Tools checked")
	fmt.Println("   ✅ .env configured")
	fmt.Println("   ✅ PostgreSQL running")
	fmt.Println("   ✅ Migrations applied")
	fmt.Println()

	printNextSteps()
	return nil
}

func printNextSteps() {
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("🎉 Setup complete! Next steps:")
	fmt.Println()
	fmt.Println("   1. Source your environment:")
	fmt.Println("      set -a && source .env && set +a")
	fmt.Println()
	fmt.Println("   2. Start the orchestrator:")
	fmt.Println("      make run")
	fmt.Println()
	fmt.Println("   3. Test it works:")
	fmt.Println("      curl http://localhost:7171/v1/health")
	fmt.Println()
	fmt.Println("   📚 Full docs: https://dev.docs.crawbl.com/getting-started")
	fmt.Println()
}

// --- Helpers ---

type toolCheck struct {
	name        string
	checkCmd    string
	installHint string
}

// checkTool runs the check command and returns true if the tool is found.
func checkTool(t toolCheck) bool {
	parts := strings.Fields(t.checkCmd)
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

// commandExists checks if a command is available on PATH.
func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// runCmd runs a command with stdout/stderr forwarded to the terminal.
func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runCmdSilent runs a command and discards all output.
func runCmdSilent(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

// copyFile copies src to dst.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(filepath.Clean(src))
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
