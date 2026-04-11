// Package out provides shared terminal output helpers for the Crawbl CLI.
package out

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"text/template"

	"github.com/charmbracelet/lipgloss"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

// V is the template variable map for rendered messages.
type V map[string]any

var (
	outWriter io.Writer = os.Stdout
	errWriter io.Writer = os.Stderr
)

var (
	lgSuccess = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	lgFailure = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	lgWarning = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	lgCyan    = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	lgCyanB   = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
)

// SetOutWriter overrides stdout output. Intended for tests.
func SetOutWriter(w io.Writer) {
	if w == nil {
		outWriter = os.Stdout
		return
	}
	outWriter = w
}

// SetErrWriter overrides stderr output. Intended for tests.
func SetErrWriter(w io.Writer) {
	if w == nil {
		errWriter = os.Stderr
		return
	}
	errWriter = w
}

// Step prints a styled message to stdout.
func Step(s style.Enum, format string, a ...any) {
	writeLine(outWriter, s, fmt.Sprintf(format, a...))
}

// Stepf renders a Go template and prints it to stdout with the selected style.
func Stepf(s style.Enum, tmpl string, v V) {
	t, err := template.New("cli").Parse(tmpl)
	if err != nil {
		Fail("invalid output template: %v", err)
		return
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, v); err != nil {
		Fail("failed to render output template: %v", err)
		return
	}

	writeLine(outWriter, s, buf.String())
}

// Ln writes a blank line to stdout.
func Ln() {
	_, _ = io.WriteString(outWriter, "\n")
}

// Prompt prints a styled prompt to stdout without a trailing newline.
func Prompt(s style.Enum, format string, a ...any) {
	write(outWriter, s, fmt.Sprintf(format, a...), false)
}

// Success prints a success message to stdout.
func Success(format string, a ...any) {
	Step(style.Success, format, a...)
}

// Warning prints a warning message to stderr.
func Warning(format string, a ...any) {
	writeLine(errWriter, style.Warning, fmt.Sprintf(format, a...))
}

// Err prints an error message to stderr.
func Err(format string, a ...any) {
	writeLine(errWriter, style.Warning, fmt.Sprintf(format, a...))
}

// Fail prints a failure message to stderr.
func Fail(format string, a ...any) {
	writeLine(errWriter, style.Failure, fmt.Sprintf(format, a...))
}

// Infof prints an indented detail line to stdout.
func Infof(format string, a ...any) {
	writeLine(outWriter, style.Indent, "  "+fmt.Sprintf(format, a...))
}

func writeLine(w io.Writer, s style.Enum, message string) {
	write(w, s, message, true)
}

func write(w io.Writer, s style.Enum, message string, newline bool) {
	line := formatLine(s, message)
	if newline && !strings.HasSuffix(line, "\n") {
		line += "\n"
	}
	_, _ = io.WriteString(w, line)
}

func formatLine(s style.Enum, message string) string {
	opts := style.Get(s)

	// Lipgloss respects NO_COLOR and terminal capability automatically.
	// Use emoji prefix when stdout is a real terminal (lipgloss HasDarkBackground
	// implies a capable terminal), falling back to LowPrefix otherwise.
	prefix := opts.LowPrefix
	if opts.Prefix != "" && lipgloss.HasDarkBackground() {
		prefix = opts.Prefix
	}

	line := message
	if prefix != "" {
		line = prefix + "  " + message
	}

	ls := lgForStyle(s)
	if ls == nil {
		return line
	}
	return ls.Render(line)
}

// lgForStyle returns the lipgloss style for a given style enum, or nil for unstyled.
func lgForStyle(s style.Enum) *lipgloss.Style {
	switch s {
	case style.Success, style.Check, style.Ready, style.Celebrate:
		return &lgSuccess
	case style.Failure, style.Destroyed:
		return &lgFailure
	case style.Warning:
		return &lgWarning
	case style.Tip, style.Doc, style.URL, style.Infra, style.Setup, style.Deploy, style.Test, style.Lint, style.Format, style.Config:
		return &lgCyan
	case style.Running, style.Database, style.Migrate, style.Docker, style.Backup, style.Reaper:
		return &lgCyanB
	default:
		return nil
	}
}
