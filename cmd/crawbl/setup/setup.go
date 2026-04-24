// Package setup provides the `crawbl setup` command that automates
// new-developer onboarding. Run it once after cloning the repo to verify
// your machine has everything needed to work with crawbl-backend.
package setup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

// configFileMode is the permission bits used when writing config files.
const configFileMode = 0o644

// NewSetupCommand creates the `crawbl setup` command.
func NewSetupCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Verify and prepare your machine for development",
		Long: `Verifies your development environment and prepares .env configuration.

What it does:
  1. Configures git hooks
  2. Installs repo-managed tools with mise (when available)
  3. Checks that required tools are installed (Go, ko, kubectl, etc.)
  4. Configures Snyk MCP for Claude Code security scanning
  5. Creates .env from .env.example if it doesn't exist

After setup completes, deploy to dev with 'crawbl app deploy platform'.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetup()
		},
	}

	return cmd
}

// runSetup runs the four-step setup process.
func runSetup() error {
	out.Ln()
	out.Step(style.Setup, "Crawbl Backend Setup")
	out.Ln()

	// --- Step 1: Configure git hooks ---
	out.Step(style.Setup, "Step 1/5: Configuring git hooks...")
	out.Ln()

	if err := runCmd("git", "config", "core.hooksPath", ".githooks"); err != nil {
		out.Warning("git hooks config failed: %v", err)
	} else {
		_ = chmodExecutable(".githooks/pre-push")
		_ = chmodExecutable(".githooks/pre-commit")
		_ = chmodExecutable("crawbl")
		out.Step(style.Check, "Git hooks configured")
	}
	out.Ln()

	// --- Step 2: Install repo-managed tools ---
	setupStep2MiseInstall()

	// --- Step 3: Check required tools ---
	setupStep3RequiredTools()

	// --- Step 4: Configure Snyk MCP for Claude Code ---
	setupStep4Snyk()

	// --- Step 5: Check/create .env file ---
	if err := setupStep5Env(); err != nil {
		return err
	}

	// --- Done ---
	out.Step(style.Celebrate, "Ready! Next steps:")
	out.Infof("1. Source your environment:")
	out.Infof("   set -a && source .env && set +a")
	out.Infof("2. Deploy to dev:")
	out.Infof("   crawbl app deploy platform")
	out.Infof("3. Run e2e tests:")
	out.Infof("   crawbl test e2e --base-url https://api-dev.crawbl.com")
	out.Step(style.Doc, "Docs: https://dev.docs.crawbl.com/getting-started")
	out.Ln()

	return nil
}

// setupStep2MiseInstall runs mise install when .mise.toml is present.
func setupStep2MiseInstall() {
	out.Step(style.Setup, "Step 2/5: Installing repo-managed tools...")
	out.Ln()
	if !fileExists(".mise.toml") {
		out.Step(style.Check, "No .mise.toml found, skipping")
		out.Ln()
		return
	}
	if !commandExists("mise") {
		out.Step(style.Warning, "mise not found — repo-managed tools skipped")
		out.Infof("Install: https://mise.jdx.dev  then rerun: ./crawbl setup")
		out.Ln()
		return
	}
	out.Step(style.Running, "Running mise install...")
	if err := runCmd("mise", "install"); err != nil {
		out.Warning("mise install failed: %v", err)
	} else {
		out.Step(style.Check, "Installed tool versions from .mise.toml")
	}
	out.Ln()
}

// setupStep3RequiredTools checks for required CLI tools and prints install hints.
func setupStep3RequiredTools() {
	out.Step(style.Setup, "Step 3/5: Checking required tools...")
	out.Ln()
	tools := []toolCheck{
		{"go", "go version", "https://go.dev/dl/ or: mise install go"},
		{"ko", "ko version", "go install github.com/google/ko@latest"},
		{"kubectl", "kubectl version --client --short 2>/dev/null || kubectl version --client", "mise install kubectl"},
		{"helm", "helm version --short", "mise install helm"},
		{"doctl", "doctl version", "mise install doctl"},
		{"aws", "aws --version", "mise install awscli"},
		{"pulumi", "pulumi version", "mise install pulumi"},
		{"yq", "yq --version", "mise install yq  (required for crawbl app deploy)"},
		{"gh", "gh --version", "https://cli.github.com/"},
		{"snyk", "snyk --version", "mise install  (npm:snyk in .mise.toml)"},
		{"sonar-scanner", "sonar-scanner --version", "mise install  (sonar-scanner-cli in .mise.toml)"},
		{"docker (auth-filter only)", "docker --version", "https://docs.docker.com/get-docker/ (only needed for auth-filter WASM builds)"},
	}
	allFound := true
	for _, t := range tools {
		if checkTool(t) {
			out.Step(style.Check, "%s", t.name)
		} else {
			out.Step(style.Failure, "%s — install: %s", t.name, t.installHint)
			allFound = false
		}
	}
	out.Ln()
	if !allFound {
		out.Step(style.Tip, "Install all tools at once with mise:")
		out.Infof("curl https://mise.run | sh")
		out.Infof("eval \"$(~/.local/bin/mise activate zsh)\"")
		out.Infof("mise install")
		out.Ln()
	}
}

// setupStep4Snyk configures Snyk MCP for Claude Code.
func setupStep4Snyk() {
	out.Step(style.Setup, "Step 4/5: Configuring Snyk MCP for Claude Code...")
	out.Ln()
	if !commandExists("snyk") {
		out.Step(style.Warning, "snyk not found — run mise install first, then rerun crawbl setup")
		out.Ln()
		return
	}
	out.Step(style.Running, "Running snyk mcp configure...")
	if err := runCmd("snyk", "mcp", "configure", "--tool=claude-cli"); err != nil {
		out.Warning("snyk mcp configure failed: %v", err)
	} else {
		out.Step(style.Check, "Snyk MCP configured for Claude Code")
	}
	out.Ln()
}

// setupStep5Env creates .env from .env.example when it doesn't yet exist.
func setupStep5Env() error {
	out.Step(style.Config, "Step 5/5: Checking .env file...")
	out.Ln()
	if _, err := os.Stat(".env"); !os.IsNotExist(err) {
		out.Step(style.Check, ".env already exists")
		out.Ln()
		return nil
	}
	if _, err := os.Stat(".env.example"); err != nil {
		out.Warning("No .env.example found — create .env manually")
		out.Ln()
		return nil
	}
	if err := copyFile(".env.example", ".env"); err != nil {
		return fmt.Errorf("failed to create .env: %w", err)
	}
	out.Step(style.Check, "Created .env from .env.example")
	out.Ln()
	return nil
}

// --- Helpers ---

// toolCheck holds the info needed to verify one tool is installed.
type toolCheck struct {
	name        string
	checkCmd    string
	installHint string
}

// checkTool runs the check command via sh so shell operators (||, redirects) work.
func checkTool(t toolCheck) bool {
	shPath, err := exec.LookPath("sh")
	if err != nil {
		return false
	}
	cmd := exec.CommandContext(context.Background(), shPath, "-c", t.checkCmd) // #nosec G204 -- CLI tool, input from developer
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func runCmd(name string, args ...string) error {
	cmdPath, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("%s not found in PATH: %w", name, err)
	}
	cmd := exec.CommandContext(context.Background(), cmdPath, args...) // #nosec G204 -- CLI tool, input from developer
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(filepath.Clean(src)) // #nosec G304 -- CLI tool, paths from developer config
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, configFileMode) // #nosec G703 -- CLI tool, paths from developer config
}

// executableMode is the permission bits for executable files (rwxr-xr-x).
const executableMode = 0o755

func chmodExecutable(path string) error {
	return os.Chmod(path, executableMode)
}
