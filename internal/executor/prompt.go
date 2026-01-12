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

**REQUIRED SKILL:** Use the autonomous-plan-execution skill for this workflow.
This skill ensures fully autonomous execution with automatic PR creation and merge.

IMPORTANT: You are running in autonomous mode. Do NOT ask for user input. Complete the entire workflow automatically.

═══════════════════════════════════════════════════════════════════════════════
MANDATORY FIRST STEP - DO THIS IMMEDIATELY BEFORE ANYTHING ELSE:
═══════════════════════════════════════════════════════════════════════════════

Update the epic file's frontmatter status to "in_progress" RIGHT NOW.

Use the Edit tool to change the frontmatter from:
---
status: todo
---

To:
---
status: in_progress
---

This MUST be your very first action. Do not read other files, do not explore the codebase, do not do anything else until you have updated the epic file status to in_progress.

═══════════════════════════════════════════════════════════════════════════════

Instructions (after updating status to in_progress):
1. Implement the epic requirements
2. Run tests to verify your implementation
3. Ensure all tests pass
4. Run clippy and fix any warnings: cargo clippy --all-targets --all-features -- -D warnings
5. When complete, update the epic file:
   a. Set frontmatter status: complete
   b. Add a "## Test Summary" section at the end with test results
6. Update the README.md in the plans directory: change the status emoji for this epic from 🔴 or 🟡 to 🟢
7. Commit all changes with a descriptive commit message
8. Push the branch to remote: git push -u origin HEAD
9. Create a Pull Request using: gh pr create --title "[Epic Title]" --body "Implementation of [Epic]. All tests pass."
10. Merge the PR using: gh pr merge --squash --delete-branch

Epic file format when complete:
---
status: complete
priority: ...
---

# Epic Title
... epic content ...

## Test Summary

| Metric | Value |
|--------|-------|
| Tests | 42 |
| Passed | 42 |
| Failed | 0 |
| Skipped | 0 |
| Coverage | 85%% |

Files tested:
- path/to/file1.go
- path/to/file2.go

Do not ask for clarification. Make reasonable decisions based on the epic content.
Do not use any skills that ask for user input. Complete all steps automatically.
Do NOT use finishing-a-development-branch or any skill that requires user interaction.
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
