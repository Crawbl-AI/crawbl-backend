package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/configenv"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/gitutil"
)

func newScanCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Run security + quality analysis on the codebase",
		Long: `Run gosec (Go security scanner) and SonarQube analysis against
the crawbl-backend source code. gosec findings are imported into
SonarQube as external security issues.

Requires sonar-scanner and gosec (install via mise install) and SONARQUBE_TOKEN.`,
		Example: `  crawbl app scan   # Run gosec + SonarQube analysis`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runScan(cmd.Context())
		},
	}

	return cmd
}

func runScan(ctx context.Context) error {
	token := configenv.StringOr("SONARQUBE_TOKEN", "")
	if token == "" {
		return fmt.Errorf("SONARQUBE_TOKEN is required (set in .env or environment)")
	}

	sonarURL := configenv.StringOr("SONARQUBE_URL", "https://sonar-dev.crawbl.com")

	for _, tool := range []string{"sonar-scanner", "gosec"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("%s is required but not found in PATH (run: mise install)", tool)
		}
	}

	rootDir, err := gitutil.RootDir()
	if err != nil {
		return fmt.Errorf("find repo root: %w", err)
	}

	// Step 1: Run gosec — produces a SonarQube-compatible JSON report.
	out.Step(style.Lint, "Running gosec security scan")
	gosecReport := "gosec-report.json"
	gosecCmd := exec.CommandContext(ctx, "gosec",
		"-fmt=sonarqube",
		"-out="+gosecReport,
		"-exclude-dir=vendor",
		"-exclude-dir=api",
		"-exclude-dir=.cache",
		"-exclude-dir=proto",
		"./...",
	)
	gosecCmd.Dir = rootDir
	gosecCmd.Stdout = os.Stdout
	gosecCmd.Stderr = os.Stderr

	// gosec exits non-zero when it finds issues — that's expected.
	// We only fail on actual execution errors (binary not found, etc.).
	if err := gosecCmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() > 0 {
			out.Infof("gosec found issues (exit code %d) — report written to %s", exitErr.ExitCode(), gosecReport)
		} else {
			return fmt.Errorf("gosec failed: %w", err)
		}
	} else {
		out.Success("gosec: no security issues found")
	}

	// Step 2: Run sonar-scanner — picks up gosec report via sonar-project.properties.
	out.Step(style.Lint, "Running SonarQube analysis")
	out.Infof("Server: %s", sonarURL)

	sonarCmd := exec.CommandContext(ctx, "sonar-scanner",
		"-Dsonar.host.url="+sonarURL,
		"-Dsonar.token="+token,
	)
	sonarCmd.Stdout = os.Stdout
	sonarCmd.Stderr = os.Stderr
	sonarCmd.Dir = rootDir

	if err := sonarCmd.Run(); err != nil {
		return fmt.Errorf("sonar-scanner failed: %w", err)
	}

	out.Success("Analysis complete")
	out.Step(style.URL, "%s/dashboard?id=crawbl-backend", sonarURL)
	return nil
}
