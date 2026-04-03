package argocd

import (
	"fmt"
	"os/exec"
)

// CheckTools verifies required CLI tools are installed.
func CheckTools() error {
	for _, tool := range []string{"yq", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("%s is required but not found in PATH (brew install %s)", tool, tool)
		}
	}
	return nil
}
