package executor

import (
	"fmt"
	"strings"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
)

const promptTemplate = `You are implementing: %s

Epic file: %s
%s
Dependencies completed: %s

Instructions:
1. First, update the epic file's frontmatter to set status: in_progress
2. Implement the epic requirements
3. Run tests to verify your implementation
4. Ensure all tests pass
5. When complete, update the epic file's frontmatter to set status: complete
6. Update the README.md in the plans directory: change the status emoji for this epic from 🔴 or 🟡 to 🟢
7. Create a summary of changes made

Status update format in epic frontmatter:
---
status: complete
priority: ...
---

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

	epicFileInfo := task.FilePath
	if epicFileInfo == "" {
		epicFileInfo = fmt.Sprintf("%s/E%02d", task.ID.Module, task.ID.EpicNum)
	}
	epicFileInfo = fmt.Sprintf("%s\n\n%s", epicFileInfo, epicContent)

	return fmt.Sprintf(promptTemplate,
		task.Title,
		epicFileInfo,
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
