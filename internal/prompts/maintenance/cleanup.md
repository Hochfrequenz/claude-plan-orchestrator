---
id: cleanup
name: Cleanup Dead Code
description: Remove unused code, fix TODOs, clean up comments
scopes: [module, package, all]
---
You are performing a code cleanup task on {{.Scope}}.

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

Start by scanning for unused code and TODOs, then clean up systematically.
