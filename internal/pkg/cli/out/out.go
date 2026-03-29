// Package out provides shared terminal output helpers for the Crawbl CLI.
package out

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"text/template"

	"golang.org/x/term"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

// V is the template variable map for rendered messages.
type V map[string]interface{}

var (
	outWriter io.Writer = os.Stdout
	errWriter io.Writer = os.Stderr
)

const (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
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
func Step(s style.Enum, format string, a ...interface{}) {
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
func Prompt(s style.Enum, format string, a ...interface{}) {
	write(outWriter, s, fmt.Sprintf(format, a...), false)
}

// Success prints a success message to stdout.
func Success(format string, a ...interface{}) {
	Step(style.Success, format, a...)
}

// Warning prints a warning message to stderr.
func Warning(format string, a ...interface{}) {
	writeLine(errWriter, style.Warning, fmt.Sprintf(format, a...))
}

// Err prints an error message to stderr.
func Err(format string, a ...interface{}) {
	writeLine(errWriter, style.Warning, fmt.Sprintf(format, a...))
}

// Fail prints a failure message to stderr.
func Fail(format string, a ...interface{}) {
	writeLine(errWriter, style.Failure, fmt.Sprintf(format, a...))
}

// Infof prints an indented detail line to stdout.
func Infof(format string, a ...interface{}) {
	writeLine(outWriter, style.Indent, "  "+fmt.Sprintf(format, a...))
}

func writeLine(w io.Writer, s style.Enum, message string) {
	write(w, s, message, true)
}

func write(w io.Writer, s style.Enum, message string, newline bool) {
	line := formatLine(s, message, useEmoji(), useColor())
	if newline && !strings.HasSuffix(line, "\n") {
		line += "\n"
	}
	_, _ = io.WriteString(w, line)
}

func formatLine(s style.Enum, message string, emojiEnabled, colorEnabled bool) string {
	opts := style.Get(s)
	prefix := opts.LowPrefix
	if emojiEnabled && opts.Prefix != "" {
		prefix = opts.Prefix
	}

	line := message
	if prefix != "" {
		line = prefix + "  " + message
	}

	if !colorEnabled {
		return line
	}

	color, bold := colorForStyle(s)
	if color == "" && !bold {
		return line
	}

	var b strings.Builder
	if bold {
		b.WriteString(colorBold)
	}
	if color != "" {
		b.WriteString(color)
	}
	b.WriteString(line)
	b.WriteString(colorReset)
	return b.String()
}

func colorForStyle(s style.Enum) (string, bool) {
	switch s {
	case style.Success, style.Check, style.Ready, style.Celebrate:
		return colorGreen, false
	case style.Failure, style.Destroyed:
		return colorRed, false
	case style.Warning:
		return colorYellow, false
	case style.Tip, style.Doc, style.URL, style.Infra, style.Setup, style.Deploy, style.Test, style.Lint, style.Format, style.Config:
		return colorCyan, false
	case style.Running, style.Database, style.Migrate, style.Docker, style.Backup, style.Reaper:
		return colorCyan, true
	default:
		return "", false
	}
}

func useEmoji() bool {
	if forced, ok := boolEnv("CRAWBL_IN_STYLE"); ok {
		return forced
	}
	if os.Getenv("CI") != "" {
		return false
	}
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return false
	}

	termEnv := strings.ToLower(strings.TrimSpace(os.Getenv("TERM")))
	if strings.Contains(termEnv, "color") || os.Getenv("COLORTERM") != "" {
		return true
	}
	return false
}

func useColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return useEmoji()
}

func boolEnv(key string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "":
		return false, false
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}
