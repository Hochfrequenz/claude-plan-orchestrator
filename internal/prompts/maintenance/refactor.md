---
id: refactor
name: Refactor Code
description: Improve code structure without changing behavior
scopes: [module, package, all]
---
You are performing a code refactoring task on {{.Scope}}.

Your goal is to improve the code structure, readability, and maintainability without changing the external behavior. Focus on:

1. Extract common patterns into helper functions
2. Reduce code duplication
3. Improve naming for clarity
4. Simplify complex conditionals
5. Break down large functions into smaller, focused ones
6. Improve error handling patterns

**Important:**
- Do NOT change the public API or behavior
- Run tests after changes to verify nothing broke
- Make incremental commits with clear messages
- Create a PR with your changes when done

Start by analyzing the codebase structure, then make targeted improvements.
