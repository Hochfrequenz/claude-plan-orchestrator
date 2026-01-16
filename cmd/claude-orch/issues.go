package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"text/tabwriter"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/issues"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/taskstore"
	"github.com/spf13/cobra"
)

var issuesCmd = &cobra.Command{
	Use:   "issues",
	Short: "Manage GitHub issue integration",
}

var issuesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tracked GitHub issues",
	RunE:  runIssuesList,
}

var issuesAnalyzeCmd = &cobra.Command{
	Use:   "analyze [issue-number]",
	Short: "Manually trigger analysis for a specific issue",
	Args:  cobra.ExactArgs(1),
	RunE:  runIssuesAnalyze,
}

func init() {
	issuesCmd.AddCommand(issuesListCmd)
	issuesCmd.AddCommand(issuesAnalyzeCmd)
	rootCmd.AddCommand(issuesCmd)
}

func runIssuesList(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	store, err := taskstore.New(cfg.General.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()

	// List all issues from DB
	issuesList, err := store.ListGitHubIssues()
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "#\tStatus\tGroup\tTitle")
	fmt.Fprintln(w, "-\t------\t-----\t-----")

	for _, issue := range issuesList {
		groupStr := "-"
		if issue.GroupName != "" {
			groupStr = issue.GroupName
		}

		fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", issue.IssueNumber, issue.Status, groupStr, truncate(issue.Title, 50))
	}
	w.Flush()

	return nil
}

func runIssuesAnalyze(cmd *cobra.Command, args []string) error {
	issueNum, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid issue number: %s", args[0])
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	if !cfg.GitHubIssues.Enabled {
		return fmt.Errorf("github_issues not enabled in config")
	}

	store, err := taskstore.New(cfg.General.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()

	// Create issue record
	issue := &domain.GitHubIssue{
		IssueNumber: issueNum,
		Repo:        cfg.GitHubIssues.Repo,
		Status:      domain.IssuePending,
	}

	analyzer := issues.NewAnalyzer(store, &cfg.GitHubIssues,
		filepath.Join(cfg.General.ProjectRoot, "docs", "plans"))

	fmt.Printf("Analyzing issue #%d...\n", issueNum)
	if err := analyzer.AnalyzeOne(cmd.Context(), issue); err != nil {
		return err
	}

	fmt.Println("Done")
	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
