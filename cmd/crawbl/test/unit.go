package test

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

const gotestfmtModule = "github.com/gotesttools/gotestfmt/v2/cmd/gotestfmt@v2.5.0"

func newUnitCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "unit",
		Short: "Run the Go unit test suite",
		Long:  "Run the Go test suite across the repository using the vendored dependency set.",
		RunE: func(cmd *cobra.Command, args []string) error {
			formatterPath, err := ensureGoTool("gotestfmt", gotestfmtModule)
			if err != nil {
				return fmt.Errorf("failed to prepare gotestfmt: %w", err)
			}

			out.Step(style.Test, "Running Go unit tests...")
			return runFormattedGoTests(formatterPath, "-hide", "empty-packages")
		},
	}
}

func runFormattedGoTests(formatterPath string, formatterArgs ...string) error {
	reader, writer := io.Pipe()

	formatter := exec.Command(formatterPath, formatterArgs...)
	formatter.Stdin = reader
	formatter.Stdout = os.Stdout
	formatter.Stderr = os.Stderr

	goTest := exec.Command("go", "test", "-mod=vendor", "-json", "./...")
	goTest.Stdout = writer
	goTest.Stderr = writer

	if err := formatter.Start(); err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return fmt.Errorf("start gotestfmt: %w", err)
	}

	if err := goTest.Start(); err != nil {
		_ = writer.Close()
		_ = formatter.Wait()
		return fmt.Errorf("start go test: %w", err)
	}

	goTestErr := goTest.Wait()
	closeErr := writer.Close()
	formatterErr := formatter.Wait()
	_ = reader.Close()

	if goTestErr != nil {
		if formatterErr != nil {
			return fmt.Errorf("go test failed: %w (formatter also failed: %v)", goTestErr, formatterErr)
		}
		return goTestErr
	}
	if closeErr != nil {
		return fmt.Errorf("close go test output pipe: %w", closeErr)
	}
	if formatterErr != nil {
		return fmt.Errorf("gotestfmt failed: %w", formatterErr)
	}

	return nil
}

func ensureGoTool(binaryName, installTarget string) (string, error) {
	if path, err := exec.LookPath(binaryName); err == nil {
		return path, nil
	}

	out.Step(style.Deploy, "Installing %s...", binaryName)
	install := exec.Command("go", "install", installTarget)
	install.Stdout = os.Stdout
	install.Stderr = os.Stderr
	if err := install.Run(); err != nil {
		return "", fmt.Errorf("go install %s: %w", installTarget, err)
	}

	if path, err := exec.LookPath(binaryName); err == nil {
		return path, nil
	}

	for _, path := range goToolCandidates(binaryName) {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("%s installed successfully but was not found on PATH", binaryName)
}

func goToolCandidates(binaryName string) []string {
	binary := binaryName
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}

	var candidates []string
	if gobin := strings.TrimSpace(goEnv("GOBIN")); gobin != "" {
		candidates = append(candidates, filepath.Join(gobin, binary))
	}

	for _, root := range filepath.SplitList(strings.TrimSpace(goEnv("GOPATH"))) {
		if root == "" {
			continue
		}
		candidates = append(candidates, filepath.Join(root, "bin", binary))
	}

	return candidates
}

func goEnv(key string) string {
	cmd := exec.Command("go", "env", key)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}
