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

NOTE: The orchestrator automatically manages epic and README status. You do NOT need to update:
- The epic file's frontmatter status (orchestrator sets it to in_progress/complete)
- The README.md status emoji (orchestrator updates it automatically)

Instructions:
1. Implement the epic requirements
2. IMPORTANT: Commit your changes before running builds/tests via MCP tools
   - The build pool workers clone the repo at HEAD, so they only see committed code
   - Use: git add -A && git commit -m "wip: [description]"
   - You can amend this commit later with a better message
3. Run tests to verify your implementation
   - PREFER using the 'test' MCP tool if available (offloads to build pool)
   - Fallback: cargo test
4. Ensure all tests pass
5. Run clippy and fix any warnings
   - PREFER using the 'clippy' MCP tool if available (offloads to build pool)
   - Fallback: cargo clippy --all-targets --all-features -- -D warnings
6. For builds, PREFER using the 'build' MCP tool if available
7. When complete, add a "## Test Summary" section at the end of the epic file with test results
8. Commit all changes with a descriptive commit message (amend the wip commit if needed)
9. Push the branch to remote: git push -u origin HEAD
10. Create a Pull Request using: gh pr create --title "[Epic Title]" --body "Implementation of [Epic]. All tests pass."
11. Merge the PR using: gh pr merge --squash --delete-branch

Test Summary format to add to epic file:

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
