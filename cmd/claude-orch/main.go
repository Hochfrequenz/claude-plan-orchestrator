package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Version information - injected at build time via ldflags
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// GetVersion returns the current version string
func GetVersion() string {
	return version
}

var (
	configPath string
	rootCmd    = &cobra.Command{
		Use:     "claude-orch",
		Short:   "Claude Plan Orchestrator - Autonomous development manager",
		Version: version,
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
