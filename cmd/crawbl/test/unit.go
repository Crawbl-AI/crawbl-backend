package test

import (
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

func newUnitCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "unit",
		Short: "Run the Go test suite",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := exec.Command("go", "test", "-mod=vendor", "./...")
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		},
	}
}
