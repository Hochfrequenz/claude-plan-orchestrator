---
id: security
name: Security Review
description: Review code for security issues
scopes: [module, package, all]
---
You are performing a security review task on {{.Scope}}.

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

Start by reviewing input handling and data flow.
