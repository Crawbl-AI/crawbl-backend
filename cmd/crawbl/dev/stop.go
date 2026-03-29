package dev

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop all local development containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("⏹️  Stopping containers...")
			return shellCmd("docker", "compose", "--profile", "default", "--profile", "database", "down", "--remove-orphans")
		},
	}
}
