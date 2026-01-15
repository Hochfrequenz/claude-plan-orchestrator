---
id: tests
name: Improve Test Coverage
description: Add tests for uncovered code paths
scopes: [module, package, all]
---
You are performing a test coverage improvement task on {{.Scope}}.

Your goal is to improve test coverage. Focus on:

1. Add unit tests for untested functions
2. Add edge case tests (empty inputs, nil, boundaries)
3. Add error path tests
4. Add integration tests for complex workflows
5. Improve test assertions to be more specific
6. Add table-driven tests where appropriate

**Important:**
- Focus on testing behavior, not implementation
- Use meaningful test names that describe what's being tested
- Keep tests focused and independent
- Run tests to ensure they pass
- Create a PR with your changes when done

Start by identifying code with low test coverage.
