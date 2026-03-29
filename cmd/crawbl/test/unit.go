package test

import (
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

func newUnitCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "unit",
		Short: "Run the Go unit test suite",
		Long:  "Run the Go test suite across the repository using the vendored dependency set.",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := exec.Command("go", "test", "-mod=vendor", "./...")
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		},
	}
}
