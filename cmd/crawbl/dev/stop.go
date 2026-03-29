package dev

import (
	"github.com/spf13/cobra"

	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/out"
	"github.com/Crawbl-AI/crawbl-backend/internal/pkg/cli/style"
)

func newStopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the local development stack",
		Long:  "Stop the local Docker Compose services used for Crawbl development.",
		RunE: func(cmd *cobra.Command, args []string) error {
			out.Step(style.Stopping, "Stopping the local development stack...")
			return shellCmd("docker", "compose", "--profile", "default", "--profile", "database", "down", "--remove-orphans")
		},
	}
}
