package release

import (
	"fmt"
	"os/exec"
)

// CheckTools verifies that claude and gh CLIs are installed.
// Called at deploy startup to fail early before building images.
func CheckTools() error {
	for _, tool := range []string{"claude", "gh"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("%s is required but not found in PATH — install it before deploying", tool)
		}
	}
	return nil
}
