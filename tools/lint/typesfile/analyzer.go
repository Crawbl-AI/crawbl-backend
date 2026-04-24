// Package typesfile provides a Go analysis linter that enforces placing
// type, const, and var declarations inside types.go files.
package typesfile

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Analyzer reports type, const, and var declarations found outside types.go.
var Analyzer = &analysis.Analyzer{
	Name: "typesfile",
	Doc:  "enforces that type, const, and var declarations live in types.go",
	Run:  run,
}

// generatedComment is the standard marker written by code generators.
const generatedComment = "Code generated"

// doNotEdit is the secondary marker required alongside generatedComment.
const doNotEdit = "DO NOT EDIT."

// maxGeneratedHeaderLines is the number of leading comment lines to inspect
// for the generated-file markers.
const maxGeneratedHeaderLines = 10

func run(pass *analysis.Pass) (any, error) {
	// Single-file packages are exempt — there is nowhere else to put declarations.
	if len(pass.Files) <= 1 {
		return nil, nil
	}

	for _, f := range pass.Files {
		name := filepath.Base(pass.Fset.File(f.Pos()).Name())
		if skipFile(name, f) {
			continue
		}

		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			if gd.Tok != token.TYPE && gd.Tok != token.CONST && gd.Tok != token.VAR {
				continue
			}
			pass.Reportf(gd.Pos(), "%s declaration must live in types.go (found in %s)", gd.Tok, name)
		}
	}

	return nil, nil
}

// skipFile reports whether a file should be excluded from analysis.
func skipFile(name string, f *ast.File) bool {
	if name == "types.go" || name == "doc.go" {
		return true
	}
	if strings.HasSuffix(name, "_test.go") {
		return true
	}
	if isGenerated(f) {
		return true
	}
	return false
}

// isGenerated reports whether f carries the standard generated-file header.
// It checks up to maxGeneratedHeaderLines comment lines at the top of the file.
func isGenerated(f *ast.File) bool {
	// ast.IsGenerated is available from Go 1.21.
	if ast.IsGenerated(f) {
		return true
	}

	checked := 0
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			if checked >= maxGeneratedHeaderLines {
				return false
			}
			if strings.Contains(c.Text, generatedComment) && strings.Contains(c.Text, doNotEdit) {
				return true
			}
			checked++
		}
	}
	return false
}
