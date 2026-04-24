package buildtools

import (
	"fmt"
	"os/exec"
)

// CheckArgoCDTools verifies required CLI tools for ArgoCD operations are installed.
func CheckArgoCDTools() error {
	for _, tool := range []string{"yq", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("%s is required but not found in PATH (brew install %s)", tool, tool)
		}
	}
	return nil
}
