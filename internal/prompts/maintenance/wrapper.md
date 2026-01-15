{{.Prompt}}

**AUTONOMOUS EXECUTION:** You are running without user interaction. Complete all steps automatically.

Instructions for autonomous execution:
1. Analyze the code in the specified scope
2. Make incremental, well-tested changes
3. Commit changes frequently with clear messages
4. Run tests after changes to verify nothing broke
5. If tests fail, fix the issues before continuing
6. When complete, push the branch and create a PR
7. Use: gh pr create --title "chore(maintenance): [brief description]" --body "Maintenance task: [details]"
8. Merge the PR using: gh pr merge --squash --delete-branch

Do not ask for clarification. Make reasonable decisions based on the codebase.
Do not use any skills that ask for user input.
