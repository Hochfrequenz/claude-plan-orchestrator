// Package maintenance provides maintenance task templates for the orchestrator.
package maintenance

// Template represents a maintenance task template
type Template struct {
	ID          string   // Unique identifier
	Name        string   // Display name
	Description string   // Short description for the UI
	Prompt      string   // Full prompt template (use {scope} and {module} placeholders)
	ScopeTypes  []string // Supported scopes: "module", "package", "all"
}

// BuiltinTemplates contains the default maintenance task templates
var BuiltinTemplates = []Template{
	{
		ID:          "refactor",
		Name:        "Refactor Code",
		Description: "Improve code structure without changing behavior",
		Prompt: `You are performing a code refactoring task on {scope}.

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

Start by analyzing the codebase structure, then make targeted improvements.`,
		ScopeTypes: []string{"module", "package", "all"},
	},
	{
		ID:          "cleanup",
		Name:        "Cleanup Dead Code",
		Description: "Remove unused code, fix TODOs, clean up comments",
		Prompt: `You are performing a code cleanup task on {scope}.

Your goal is to remove dead code and improve code hygiene. Focus on:

1. Remove unused functions, variables, and imports
2. Address or remove TODO/FIXME comments (implement if simple, remove if obsolete)
3. Remove commented-out code blocks
4. Clean up debug logging that shouldn't be in production
5. Remove duplicate or redundant code
6. Fix obvious typos in comments and strings

**Important:**
- Be conservative - only remove code you're certain is unused
- Run tests after changes to verify nothing broke
- Make incremental commits with clear messages
- Create a PR with your changes when done

Start by scanning for unused code and TODOs, then clean up systematically.`,
		ScopeTypes: []string{"module", "package", "all"},
	},
	{
		ID:          "optimize",
		Name:        "Optimize Performance",
		Description: "Identify and fix performance bottlenecks",
		Prompt: `You are performing a performance optimization task on {scope}.

Your goal is to identify and fix performance issues. Focus on:

1. Reduce unnecessary allocations
2. Optimize hot paths and loops
3. Add caching where appropriate
4. Reduce database/network round trips
5. Use more efficient data structures
6. Parallelize independent operations where safe

**Important:**
- Profile before optimizing - focus on actual bottlenecks
- Document performance improvements with benchmarks if possible
- Don't sacrifice readability for micro-optimizations
- Run tests after changes to verify correctness
- Create a PR with your changes when done

Start by identifying the most impactful optimization opportunities.`,
		ScopeTypes: []string{"module", "package", "all"},
	},
	{
		ID:          "docs",
		Name:        "Improve Documentation",
		Description: "Add or improve code documentation",
		Prompt: `You are performing a documentation improvement task on {scope}.

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

Start by identifying undocumented or poorly documented public APIs.`,
		ScopeTypes: []string{"module", "package", "all"},
	},
	{
		ID:          "tests",
		Name:        "Improve Test Coverage",
		Description: "Add tests for uncovered code paths",
		Prompt: `You are performing a test coverage improvement task on {scope}.

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

Start by identifying code with low test coverage.`,
		ScopeTypes: []string{"module", "package", "all"},
	},
	{
		ID:          "security",
		Name:        "Security Review",
		Description: "Review code for security issues",
		Prompt: `You are performing a security review task on {scope}.

Your goal is to identify and fix security issues. Focus on:

1. Input validation and sanitization
2. SQL injection vulnerabilities
3. Command injection risks
4. Path traversal vulnerabilities
5. Sensitive data exposure (logs, errors)
6. Authentication/authorization issues
7. Cryptographic weaknesses
8. Race conditions in security-critical code

**Important:**
- Document any security issues found
- Prioritize fixes by severity
- Don't introduce new vulnerabilities while fixing
- Run tests after changes
- Create a PR with your changes when done
- Flag any critical issues that need immediate attention

Start by reviewing input handling and data flow.`,
		ScopeTypes: []string{"module", "package", "all"},
	},
	{
		ID:          "lint",
		Name:        "Fix Linting Issues",
		Description: "Fix code style and linting warnings",
		Prompt: `You are performing a linting cleanup task on {scope}.

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

Start by running the linter and addressing issues systematically.`,
		ScopeTypes: []string{"module", "package", "all"},
	},
	{
		ID:          "custom",
		Name:        "Custom Task",
		Description: "Define your own maintenance task",
		Prompt:      "", // User provides custom prompt
		ScopeTypes:  []string{"module", "package", "all"},
	},
}

// GetTemplate returns a template by ID, or nil if not found
func GetTemplate(id string) *Template {
	for i := range BuiltinTemplates {
		if BuiltinTemplates[i].ID == id {
			return &BuiltinTemplates[i]
		}
	}
	return nil
}

// ScopeSupported checks if a template supports a given scope type
func (t *Template) ScopeSupported(scope string) bool {
	for _, s := range t.ScopeTypes {
		if s == scope {
			return true
		}
	}
	return false
}
