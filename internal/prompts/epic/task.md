You are implementing: {{.Title}}

Epic file: {{.EpicFilePath}}

{{.EpicContent}}
{{if .ModuleContext}}
Module context:
{{.ModuleContext}}
{{end}}
Dependencies completed: {{.CompletedDeps}}

**REQUIRED SKILL:** Use the autonomous-plan-execution skill for this workflow.
This skill ensures fully autonomous execution with automatic PR creation and merge.

IMPORTANT: You are running in autonomous mode. Do NOT ask for user input. Complete the entire workflow automatically.

NOTE: The orchestrator automatically manages epic and README status. You do NOT need to update:
- The epic file's frontmatter status (orchestrator sets it to in_progress/complete)
- The README.md status emoji (orchestrator updates it automatically)

Instructions:
1. Initialize any git submodules: git submodule update --init --recursive
2. Implement the epic requirements
3. Run tests to verify your implementation
   - Note: The build MCP tools auto-commit uncommitted changes before building
   - PREFER using the 'test' MCP tool if available (offloads to build pool)
   - Fallback: cargo test
4. Ensure all tests pass
5. Run clippy and fix any warnings
   - PREFER using the 'clippy' MCP tool if available (offloads to build pool)
   - Fallback: cargo clippy --all-targets --all-features -- -D warnings
6. For builds, PREFER using the 'build' MCP tool if available
7. When complete, add a "## Test Summary" section at the end of the epic file with test results
8. Commit all changes with a descriptive commit message
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
| Coverage | 85% |

Files tested:
- path/to/file1.go
- path/to/file2.go

Do not ask for clarification. Make reasonable decisions based on the epic content.
Do not use any skills that ask for user input. Complete all steps automatically.
Do NOT use finishing-a-development-branch or any skill that requires user interaction.
