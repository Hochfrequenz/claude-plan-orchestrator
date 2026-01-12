package executor

import (
	"fmt"
	"strings"

	"github.com/anthropics/erp-orchestrator/internal/domain"
)

const promptTemplate = `You are implementing: %s

Epic: %s
%s
Dependencies completed: %s

Instructions:
1. Implement the epic requirements
2. Run tests to verify your implementation
3. Ensure all tests pass
4. When complete, create a summary of changes made

Do not ask for clarification. Make reasonable decisions based on the epic content.
`

// BuildPrompt constructs the task prompt for Claude Code
func BuildPrompt(task *domain.Task, epicContent, moduleOverview string, completedDeps []string) string {
	var moduleCtx string
	if moduleOverview != "" {
		moduleCtx = fmt.Sprintf("\nModule context:\n%s\n", moduleOverview)
	}

	depsStr := "None"
	if len(completedDeps) > 0 {
		depsStr = strings.Join(completedDeps, ", ")
	}

	return fmt.Sprintf(promptTemplate,
		task.Title,
		epicContent,
		moduleCtx,
		depsStr,
	)
}

// BuildCommitMessage creates the commit message format
func BuildCommitMessage(task *domain.Task, summary string) string {
	return fmt.Sprintf("feat(%s): implement E%02d - %s\n\n%s",
		task.ID.Module,
		task.ID.EpicNum,
		task.Title,
		summary,
	)
}
