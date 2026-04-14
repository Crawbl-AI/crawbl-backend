package typesfile

import (
	"golang.org/x/tools/go/analysis"

	"github.com/golangci/plugin-module-register/register"
)

func init() {
	register.Plugin("typesfile", New)
}

// plugin implements the golangci-lint module plugin contract.
type plugin struct{}

// New constructs the typesfile linter plugin. It accepts an optional
// configuration value (unused — the rule is hard-coded).
func New(_ any) (register.LinterPlugin, error) {
	return &plugin{}, nil
}

// BuildAnalyzers returns the list of analyzers provided by this plugin.
func (p *plugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{Analyzer}, nil
}

// GetLoadMode returns the minimum load mode required by the analyzers.
// Syntax-only is sufficient because the rule inspects AST tokens.
func (p *plugin) GetLoadMode() string {
	return register.LoadModeSyntax
}
