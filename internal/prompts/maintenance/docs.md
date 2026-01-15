---
id: docs
name: Improve Documentation
description: Add or improve code documentation
scopes: [module, package, all]
---
You are performing a documentation improvement task on {{.Scope}}.

Your goal is to improve code documentation. Focus on:

1. Add package-level documentation explaining purpose and usage
2. Document exported functions with clear descriptions
3. Add examples for complex functions
4. Document non-obvious behavior or edge cases
5. Add inline comments for complex algorithms
6. Ensure error conditions are documented

**Important:**
- Follow Go documentation conventions
- Keep documentation concise but complete
- Don't document the obvious (e.g., "NewFoo creates a new Foo")
- Focus on the "why" not just the "what"
- Create a PR with your changes when done

Start by identifying undocumented or poorly documented public APIs.
