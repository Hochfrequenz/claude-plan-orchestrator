---
id: lint
name: Fix Linting Issues
description: Fix code style and linting warnings
scopes: [module, package, all]
---
You are performing a linting cleanup task on {{.Scope}}.

Your goal is to fix code style issues and linting warnings. Focus on:

1. Run golangci-lint and fix reported issues
2. Fix formatting inconsistencies
3. Address staticcheck warnings
4. Fix ineffective assignments
5. Address unused parameter warnings
6. Fix error handling (don't ignore errors)

**Important:**
- Follow the project's existing code style
- Don't change behavior while fixing style
- Run tests after changes
- Create a PR with your changes when done

Start by running the linter and addressing issues systematically.
