package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	configPath string
	rootCmd    = &cobra.Command{
		Use:   "claude-orch",
		Short: "Claude Plan Orchestrator - Autonomous development manager",
		Long: `Claude Plan Orchestrator manages Claude Code agents working on development tasks.
It parses markdown plans, dispatches work to agents in git worktrees,
and handles the full PR lifecycle through to merge.`,
	}
)

func init() {
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "config file path")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
