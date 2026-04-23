package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
		Long: `Run gosec (Go security scanner), staticcheck (Go static analysis),
and SonarQube analysis against the crawbl-backend source code.
Findings are imported into SonarQube as external issues.

Requires sonar-scanner, gosec, and staticcheck (install via mise install) and SONARQUBE_TOKEN.`,
		Example: `  crawbl app scan   # Run gosec + staticcheck + SonarQube analysis`,
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

	for _, tool := range []string{"sonar-scanner", "gosec", "staticcheck"} {
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

	// Step 2: Run staticcheck — produces JSON output converted to SonarQube format.
	out.Step(style.Lint, "Running staticcheck analysis")
	staticcheckReport := "staticcheck-report.json"
	if err := runStaticcheck(ctx, rootDir, staticcheckReport); err != nil {
		out.Warning("staticcheck failed: %v", err)
	}

	// Step 3: Run sonar-scanner — picks up external reports via sonar-project.properties.
	out.Step(style.Lint, "Running SonarQube analysis")
	out.Infof("Server: %s", sonarURL)

	sonarCmd := exec.CommandContext(ctx, "sonar-scanner", // #nosec G204 -- CLI tool, input from developer
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

// runStaticcheck runs staticcheck with JSON output and converts the results
// to SonarQube's generic issue import format.
func runStaticcheck(ctx context.Context, rootDir, reportPath string) error {
	scPath, err := exec.LookPath("staticcheck")
	if err != nil {
		return fmt.Errorf("staticcheck not found in PATH: %w", err)
	}

	var buf bytes.Buffer
	scCmd := exec.CommandContext(ctx, scPath, "-f", "json", "./...")
	scCmd.Dir = rootDir
	scCmd.Stdout = &buf
	scCmd.Stderr = os.Stderr

	// staticcheck exits non-zero when it finds issues — that's expected.
	if err := scCmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			return fmt.Errorf("run staticcheck: %w", err)
		}
	}

	report, err := convertStaticcheckToSonar(buf.Bytes(), rootDir)
	if err != nil {
		return fmt.Errorf("convert staticcheck output: %w", err)
	}

	if err := os.WriteFile(filepath.Join(rootDir, reportPath), report, 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}

	out.Success("staticcheck: report written to %s", reportPath)
	return nil
}

// staticcheckDiag represents a single staticcheck JSON diagnostic.
type staticcheckDiag struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Location struct {
		File   string `json:"file"`
		Line   int    `json:"line"`
		Column int    `json:"column"`
	} `json:"location"`
	End struct {
		File   string `json:"file"`
		Line   int    `json:"line"`
		Column int    `json:"column"`
	} `json:"end"`
	Message string `json:"message"`
}

// sonarGenericReport is the SonarQube generic issue import format.
type sonarGenericReport struct {
	Issues []sonarGenericIssue `json:"issues"`
}

// sonarGenericIssue is a single issue in the SonarQube generic format.
type sonarGenericIssue struct {
	EngineID        string              `json:"engineId"`
	RuleID          string              `json:"ruleId"`
	Severity        string              `json:"severity"`
	Type            string              `json:"type"`
	PrimaryLocation sonarGenericLocation `json:"primaryLocation"`
}

// sonarGenericLocation describes where an issue occurs.
type sonarGenericLocation struct {
	Message   string               `json:"message"`
	FilePath  string               `json:"filePath"`
	TextRange sonarGenericTextRange `json:"textRange"`
}

// sonarGenericTextRange describes the text range of an issue.
type sonarGenericTextRange struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn,omitempty"`
	EndLine     int `json:"endLine,omitempty"`
	EndColumn   int `json:"endColumn,omitempty"`
}

// convertStaticcheckToSonar converts staticcheck JSON-lines output to
// SonarQube's generic issue import format.
func convertStaticcheckToSonar(data []byte, rootDir string) ([]byte, error) {
	var report sonarGenericReport

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var diag staticcheckDiag
		if err := json.Unmarshal(line, &diag); err != nil {
			continue
		}

		// Convert absolute paths to repo-relative paths.
		relPath, err := filepath.Rel(rootDir, diag.Location.File)
		if err != nil {
			relPath = diag.Location.File
		}

		// Skip vendor and generated files.
		if strings.HasPrefix(relPath, "vendor/") || strings.HasSuffix(relPath, ".pb.go") {
			continue
		}

		report.Issues = append(report.Issues, sonarGenericIssue{
			EngineID: "staticcheck",
			RuleID:   diag.Code,
			Severity: staticcheckSeverityToSonar(diag.Severity),
			Type:     staticcheckCodeToType(diag.Code),
			PrimaryLocation: sonarGenericLocation{
				Message:  diag.Message,
				FilePath: relPath,
				TextRange: sonarGenericTextRange{
					StartLine: diag.Location.Line,
				},
			},
		})
	}

	out.Infof("staticcheck found %d issues", len(report.Issues))
	return json.MarshalIndent(report, "", "  ")
}

func staticcheckSeverityToSonar(severity string) string {
	switch severity {
	case "error":
		return "MAJOR"
	case "warning":
		return "MINOR"
	default:
		return "INFO"
	}
}

func staticcheckCodeToType(code string) string {
	switch {
	case strings.HasPrefix(code, "SA"):
		return "BUG"
	case strings.HasPrefix(code, "S1"):
		return "CODE_SMELL"
	case strings.HasPrefix(code, "ST"):
		return "CODE_SMELL"
	case strings.HasPrefix(code, "QF"):
		return "CODE_SMELL"
	default:
		return "CODE_SMELL"
	}
}
