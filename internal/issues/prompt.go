// internal/issues/prompt.go
package issues

import "fmt"

const analysisPromptTemplate = `You are analyzing GitHub issue #%d from repository %s for implementation readiness.

## Your Task

1. Use the GitHub MCP tools to fetch the full issue details (title, body, comments, labels)
2. Evaluate against the readiness checklist below
3. Scan existing plans in %s to identify potential dependencies
4. Output your analysis as JSON

## Readiness Checklist

Evaluate each criterion:
- problem_statement: Is there a clear description of what problem needs to be solved?
- acceptance_criteria: Are there defined success criteria or expected outcomes?
- bounded_scope: Is the scope limited and achievable (not open-ended)?
- no_blocking_questions: Are there unanswered questions that block implementation?
- files_identified: Can you identify which files/areas of code are affected?

## Output Format

Return a JSON object with this exact structure:
` + "```json" + `
{
  "issue_number": %d,
  "ready": true/false,
  "checklist": {
    "problem_statement": { "pass": true/false, "notes": "explanation" },
    "acceptance_criteria": { "pass": true/false, "notes": "explanation" },
    "bounded_scope": { "pass": true/false, "notes": "explanation" },
    "no_blocking_questions": { "pass": true/false, "notes": "explanation" },
    "files_identified": { "pass": true/false, "notes": "list of files or areas" }
  },
  "group": "area-label-value or empty string",
  "plan_files": ["path/to/epic.md"],
  "dependencies": ["module/E##"],
  "comment_posted": true,
  "labels_updated": true,
  "refinement_suggestions": ["suggestion 1", "suggestion 2"]
}
` + "```" + `

## If NOT Ready

- Post a comment explaining what information is missing
- Include specific suggestions for improvement
- Add label: needs-refinement
- Remove label: orchestrator-candidate

## If Ready

- Generate an implementation plan as a markdown file
- Write it to: docs/plans/{group}/issue-%d/epic-00-{slug}.md
- The group comes from area:X label, or use "issue-%d" if no area label
- Post a comment confirming the plan was created
- Add label: implementation-ready
- Remove label: orchestrator-candidate

## Plan Format

Use this frontmatter:
` + "```yaml" + `
---
status: not_started
priority: medium
depends_on:
  - module/E## (if any dependencies found)
needs_review: false
github_issue: %d
---
` + "```" + `

Now analyze issue #%d and produce your output.
`

func BuildAnalysisPrompt(issueNumber int, repo, plansDir string) string {
	return fmt.Sprintf(analysisPromptTemplate,
		issueNumber, repo, plansDir,
		issueNumber,
		issueNumber, issueNumber,
		issueNumber,
		issueNumber)
}
