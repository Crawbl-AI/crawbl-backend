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
		Short: "Run SonarQube static analysis on the codebase",
		Long: `Run the SonarQube scanner against the crawbl-backend source code.
Results are pushed to the SonarQube server. Configuration is in
sonar-project.properties at the repo root.

Requires sonar-scanner (install via mise install) and SONARQUBE_TOKEN.`,
		Example: `  crawbl app scan   # Scan the codebase and push results to SonarQube`,
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

	if _, err := exec.LookPath("sonar-scanner"); err != nil {
		return fmt.Errorf("sonar-scanner is required but not found in PATH (run: mise install)")
	}

	rootDir, err := gitutil.RootDir()
	if err != nil {
		return fmt.Errorf("find repo root: %w", err)
	}

	out.Step(style.Lint, "Running SonarQube analysis")
	out.Infof("Server: %s", sonarURL)
	out.Infof("Project: crawbl-backend")

	// sonar-project.properties handles all configuration.
	// Only the server URL and token are passed via CLI (from env vars).
	args := []string{
		"-Dsonar.host.url=" + sonarURL,
		"-Dsonar.token=" + token,
	}

	cmd := exec.CommandContext(ctx, "sonar-scanner", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = rootDir

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sonar-scanner failed: %w", err)
	}

	out.Success("SonarQube analysis complete")
	out.Step(style.URL, "%s/dashboard?id=crawbl-backend", sonarURL)
	return nil
}
