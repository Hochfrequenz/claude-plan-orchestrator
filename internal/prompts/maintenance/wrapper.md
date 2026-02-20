{{.Prompt}}

**AUTONOMOUS EXECUTION:** You are running without user interaction. Complete all steps automatically.

Instructions for autonomous execution:
1. Initialize any git submodules: git submodule update --init --recursive
2. Analyze the code in the specified scope
3. Make incremental, well-tested changes
4. Commit changes frequently with clear messages
5. Run tests after changes to verify nothing broke
6. If tests fail, fix the issues before continuing
7. When complete, push the branch and create a PR
8. Use: gh pr create --title "chore(maintenance): [brief description]" --body "Maintenance task: [details]"
9. Merge the PR using: gh pr merge --squash --delete-branch

Do not ask for clarification. Make reasonable decisions based on the codebase.
Do not use any skills that ask for user input.
