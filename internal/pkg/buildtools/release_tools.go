package buildtools

import (
	"fmt"
	"os/exec"
)

// CheckReleaseTools verifies that the gh CLI is installed.
// Called at deploy startup to fail early before building images.
func CheckReleaseTools() error {
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("gh is required but not found in PATH — install it before deploying")
	}
	return nil
}
