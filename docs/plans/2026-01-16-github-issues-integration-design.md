# GitHub Issues Integration Design

## Overview

Enable GitHub issues as an external task source for the Claude Plan Orchestrator. An LLM agent analyzes issues for implementation readiness, generates plans, and integrates them into the existing workflow.

**Key Principles:**
- GitHub issues are an **intake mechanism**, not source of truth
- Plans are written to markdown files, reusing existing infrastructure
- Agents use GitHub MCP tools for all GitHub interactions
- Respects existing `max_parallel_agents` configuration

## Data Flow

```
┌─────────────────────────────────────────────────────────────────────────┐
│                           SYNC TRIGGERED                                 │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  1. FETCH: Get issues labeled `orchestrator-candidate` via GitHub API   │
│     - Skip issues already labeled `implementation-ready` or             │
│       `needs-refinement`                                                │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  2. ANALYZE: Spawn Claude Code agents (up to max_parallel_agents)       │
│     - Agent uses GitHub MCP to read issue details                       │
│     - Evaluates against readiness checklist                             │
│     - Posts comment with findings                                       │
│     - Updates label: `implementation-ready` or `needs-refinement`       │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  3. GENERATE: For ready issues, agent creates implementation plan       │
│     - Writes markdown to docs/plans/{group}/issue-{N}/epic-00-*.md      │
│     - Group = area label if present, else `issue-{N}`                   │
│     - Plan includes dependencies on existing module tasks if needed     │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  3b. PARSE & UPSERT (automatic, in-process)                             │
│     - parser.ParseEpicFile(filePath)                                    │
│     - store.UpsertTask(task)                                            │
│     - No separate sync command needed                                   │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  4. INTEGRATE: Task now in scheduler queue                              │
│     - Executes when dependencies met                                    │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  5. CLOSE: After PR merges, update GitHub issue                         │
│     - Post summary comment with PR link and changed files               │
│     - Close the issue                                                   │
└─────────────────────────────────────────────────────────────────────────┘
```

## Issue Analysis Agent

**Agent Type:** Claude Code subprocess (consistent with task execution agents)

**Invocation:** The Go orchestrator spawns the agent with a structured prompt containing:
- Issue number and repository
- Instructions to use GitHub MCP tools
- Readiness checklist to evaluate against
- Output format specification

### Agent Workflow

```
┌─────────────────────────────────────────────────────────────────────────┐
│  ANALYSIS AGENT WORKFLOW                                                │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                         │
│  1. FETCH ISSUE DETAILS (via GitHub MCP)                                │
│     - Title, body, comments                                             │
│     - Labels (especially area:* labels)                                 │
│     - Linked PRs or issues                                              │
│                                                                         │
│  2. EVALUATE READINESS CHECKLIST                                        │
│     □ Clear problem statement                                           │
│     □ Acceptance criteria defined                                       │
│     □ Scope is bounded (not open-ended)                                 │
│     □ No blocking questions unanswered                                  │
│     □ Relevant files/areas identified or identifiable                   │
│                                                                         │
│  3. SCAN EXISTING PLANS (read docs/plans/)                              │
│     - Identify potential dependencies                                   │
│     - Check for overlapping/duplicate work                              │
│                                                                         │
│  4. PRODUCE OUTPUT                                                      │
│     If NOT ready:                                                       │
│       - Post comment explaining gaps                                    │
│       - Suggest specific improvements                                   │
│       - Add label: `needs-refinement`                                   │
│       - Remove label: `orchestrator-candidate`                          │
│                                                                         │
│     If READY:                                                           │
│       - Generate implementation plan (markdown)                         │
│       - Write to docs/plans/{group}/issue-{N}/epic-00-*.md              │
│       - Post comment confirming plan created                            │
│       - Add label: `implementation-ready`                               │
│       - Remove label: `orchestrator-candidate`                          │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
```

### Agent Output Contract

The agent returns structured JSON so the Go code can proceed:

```json
{
  "issue_number": 42,
  "ready": true,
  "checklist": {
    "problem_statement": { "pass": true, "notes": "" },
    "acceptance_criteria": { "pass": true, "notes": "" },
    "bounded_scope": { "pass": true, "notes": "" },
    "no_blocking_questions": { "pass": true, "notes": "" },
    "files_identified": { "pass": true, "notes": "src/api/billing.go" }
  },
  "group": "billing",
  "plan_files": ["docs/plans/billing/issue-42/epic-00-add-retry-logic.md"],
  "dependencies": ["billing/E03"],
  "comment_posted": true,
  "labels_updated": true
}
```

## Generated Plan Format

**File Location:**
```
docs/plans/{group}/issue-{N}/epic-{NN}-{slug}.md
```

**Group Resolution:**
- Issue #42 with label `area:billing` → `billing/issue-42/E00`
- Issue #105 with no area label → `issue-105/E00`

**Markdown Structure:**

```markdown
---
# YAML Frontmatter
status: not_started
priority: medium
depends_on:
  - billing/E03
needs_review: false
github_issue: 42
---

# Add Retry Logic to Invoice API

## Source Issue

GitHub Issue #42: "Invoice API fails silently on timeout"

## Problem Statement

[Extracted/summarized from issue]

## Acceptance Criteria

- [ ] Invoice API retries up to 3 times on timeout
- [ ] Exponential backoff between retries
- [ ] Failed requests logged with context
- [ ] Existing tests updated

## Implementation Approach

1. Add retry wrapper in `src/api/billing.go`
2. Configure retry policy via existing config system
3. Add structured logging for retry attempts
4. Update integration tests

## Files to Modify

- `src/api/billing.go` - Add retry logic
- `src/config/config.go` - Add retry settings
- `src/api/billing_test.go` - Test coverage

## Testing Strategy

- Unit tests for retry logic
- Integration test for timeout scenarios
```

**Key Frontmatter Fields:**

| Field | Purpose |
|-------|---------|
| `github_issue` | Links back to source issue (new field) |
| `depends_on` | Cross-references to module tasks |
| `status` | Starts as `not_started` |
| `priority` | Inherited from issue labels or defaults to `medium` |

## Issue Closure After Implementation

**Trigger:** PR merged for a task that has `github_issue` field in its frontmatter.

**Location:** Extends existing `internal/prbot/` logic.

### Closure Flow

```
┌─────────────────────────────────────────────────────────────────────────┐
│  PR MERGED (existing prbot flow)                                        │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  CHECK: Does task have `github_issue` field?                            │
│     - No  → Standard completion (update markdown, done)                 │
│     - Yes → Continue to issue closure                                   │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  GATHER: Collect closure information                                    │
│     - PR number and URL                                                 │
│     - Changed files (from PR diff)                                      │
│     - Commit summary                                                    │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  POST COMMENT (via GitHub MCP/API):                                     │
│                                                                         │
│    ✅ **Implemented in PR #123**                                        │
│                                                                         │
│    **Summary:** Added retry logic with exponential backoff              │
│                                                                         │
│    **Changed files:**                                                   │
│    - `src/api/billing.go`                                               │
│    - `src/config/config.go`                                             │
│    - `src/api/billing_test.go`                                          │
│                                                                         │
│    ---                                                                  │
│    *Implemented by Claude Plan Orchestrator*                            │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  CLOSE ISSUE (via GitHub MCP/API)                                       │
│     - Close as completed                                                │
│     - Add label: `implemented` (optional, for tracking)                 │
└─────────────────────────────────────────────────────────────────────────┘
```

### Edge Cases

| Scenario | Handling |
|----------|----------|
| Multi-epic issue | Close only when ALL epics complete |
| PR closed without merge | Don't close issue; task marked failed |
| Issue already closed | Skip closure, log warning |
| GitHub API failure | Retry with backoff; log if persistent |

## Data Model & Storage Changes

### New Database Table: `github_issues`

Tracks issue analysis state to avoid re-processing:

```sql
CREATE TABLE github_issues (
    issue_number  INTEGER PRIMARY KEY,
    repo          TEXT NOT NULL,           -- "owner/repo"
    title         TEXT NOT NULL,
    status        TEXT NOT NULL,           -- "pending", "ready", "needs_refinement", "implemented"
    group_name    TEXT,                    -- resolved group (from area label or issue-{N})
    analyzed_at   TIMESTAMP,
    plan_path     TEXT,                    -- path to generated markdown (if ready)
    closed_at     TIMESTAMP,
    pr_number     INTEGER,                 -- implementing PR (after merge)
    created_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

### Task Table Addition

Add column to link tasks back to source issues:

```sql
ALTER TABLE tasks ADD COLUMN github_issue INTEGER REFERENCES github_issues(issue_number);
```

### New Store Methods

```go
// internal/taskstore/store.go additions

// Issue tracking
func (s *Store) UpsertGitHubIssue(issue *GitHubIssue) error
func (s *Store) GetGitHubIssue(issueNumber int) (*GitHubIssue, error)
func (s *Store) ListPendingIssues(repo string) ([]*GitHubIssue, error)
func (s *Store) UpdateIssueStatus(issueNumber int, status string) error
func (s *Store) MarkIssueClosed(issueNumber int, prNumber int) error

// Query helpers
func (s *Store) GetTasksByGitHubIssue(issueNumber int) ([]*Task, error)
func (s *Store) GetIncompleteEpicsForIssue(issueNumber int) ([]*Task, error)
```

### Domain Types

```go
// internal/domain/github_issue.go

type GitHubIssue struct {
    IssueNumber int
    Repo        string
    Title       string
    Status      IssueStatus  // Pending, Ready, NeedsRefinement, Implemented
    GroupName   string
    AnalyzedAt  *time.Time
    PlanPath    string
    ClosedAt    *time.Time
    PRNumber    *int
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type IssueStatus string

const (
    IssuePending         IssueStatus = "pending"
    IssueReady           IssueStatus = "ready"
    IssueNeedsRefinement IssueStatus = "needs_refinement"
    IssueImplemented     IssueStatus = "implemented"
)
```

## Configuration

**Additions to `~/.config/claude-orchestrator/config.toml`:**

```toml
# Existing config...
max_parallel_agents = 3

# New section for GitHub issue integration
[github_issues]
enabled = true
repo = "owner/repo-name"              # Target repository

# Labels used for workflow
candidate_label = "orchestrator-candidate"
ready_label = "implementation-ready"
refinement_label = "needs-refinement"
implemented_label = "implemented"     # Applied on closure

# Optional: label prefix for area detection
area_label_prefix = "area:"           # e.g., "area:billing" → group "billing"

# Optional: priority mapping from GitHub labels
[github_issues.priority_labels]
high = "priority:high"
medium = "priority:medium"
low = "priority:low"
```

### Environment Requirements

| Requirement | Purpose |
|-------------|---------|
| `gh` CLI authenticated | GitHub API access for MCP tools |
| GitHub MCP server configured | Agent uses it for issue operations |

### Validation on Startup

```go
func (c *Config) ValidateGitHubIssues() error {
    if !c.GitHubIssues.Enabled {
        return nil
    }
    if c.GitHubIssues.Repo == "" {
        return errors.New("github_issues.repo is required when enabled")
    }
    // Verify gh CLI is authenticated
    if err := exec.Command("gh", "auth", "status").Run(); err != nil {
        return errors.New("gh CLI not authenticated; run 'gh auth login'")
    }
    return nil
}
```

## New Package Structure & CLI Changes

### New Package: `internal/issues/`

```
internal/issues/
├── analyzer.go      # Orchestrates issue analysis (spawns agents)
├── fetcher.go       # GitHub API interactions (list candidates, update labels)
├── closer.go        # Post-merge issue closure logic
└── prompt.go        # Agent prompt templates
```

### Key Functions

```go
// internal/issues/analyzer.go

type Analyzer struct {
    store    *taskstore.Store
    executor *executor.Executor
    config   *config.GitHubIssuesConfig
}

// Called during sync when github_issues.enabled = true
func (a *Analyzer) AnalyzeCandidates(ctx context.Context) error

// Spawns single analysis agent, returns result
func (a *Analyzer) analyzeIssue(ctx context.Context, issue *GitHubIssue) (*AnalysisResult, error)

// Parses agent output, writes markdown, upserts task
func (a *Analyzer) processReadyIssue(ctx context.Context, result *AnalysisResult) error
```

```go
// internal/issues/closer.go

type Closer struct {
    store  *taskstore.Store
    config *config.GitHubIssuesConfig
}

// Called by prbot after PR merge
func (c *Closer) CloseIfComplete(ctx context.Context, task *domain.Task, pr *domain.PR) error

// Checks if all epics for an issue are complete
func (c *Closer) allEpicsComplete(ctx context.Context, issueNumber int) (bool, error)
```

### CLI Changes

```bash
# Sync now includes issue analysis (when enabled)
claude-orch sync                    # Parses markdown AND analyzes candidate issues

# New flags for sync
claude-orch sync --skip-issues      # Skip issue analysis this run
claude-orch sync --issues-only      # Only analyze issues, skip markdown sync

# New subcommand for issue management
claude-orch issues list             # Show tracked issues and their status
claude-orch issues analyze 42       # Manually trigger analysis for specific issue
claude-orch issues close 42         # Manually close issue (if implementation done)
```

### Integration Points

| Location | Change |
|----------|--------|
| `cmd/claude-orch/sync.go` | Call `issues.Analyzer.AnalyzeCandidates()` |
| `internal/prbot/merge.go` | Call `issues.Closer.CloseIfComplete()` after merge |
| `internal/sync/sync.go` | No changes (markdown sync unchanged) |

## TUI Integration

### Task List Enhancements

```
┌─────────────────────────────────────────────────────────────────────────┐
│  QUEUED TASKS                                                           │
├─────────────────────────────────────────────────────────────────────────┤
│  ▸ billing/E04        Add invoice templates          ready              │
│  ▸ billing/issue-42   Add retry logic        #42     ready              │
│    ────────────────── ↑ shows issue number ──────────                   │
│  ▸ issue-105/E00      Fix auth timeout       #105    blocked (auth/E02) │
└─────────────────────────────────────────────────────────────────────────┘
```

### Task Detail View (extended)

```
┌─────────────────────────────────────────────────────────────────────────┐
│  TASK: billing/issue-42/E00                                             │
├─────────────────────────────────────────────────────────────────────────┤
│  Title:     Add retry logic to Invoice API                              │
│  Status:    not_started                                                 │
│  Priority:  medium                                                      │
│  Source:    GitHub Issue #42                                            │
│             https://github.com/owner/repo/issues/42                     │
│  Depends:   billing/E03                                                 │
│                                                                         │
│  [p] View Prompt  [o] View Output  [i] Open Issue                       │
└─────────────────────────────────────────────────────────────────────────┘
```

### New: Issues Tab (optional, low priority)

```
┌─────────────────────────────────────────────────────────────────────────┐
│  Dashboard │ Modules │ Queued │ Active │ PRs │ Issues                   │
├─────────────────────────────────────────────────────────────────────────┤
│  #42   billing     ready             Analyzed 2h ago                    │
│  #105  issue-105   ready             Analyzed 1h ago                    │
│  #108  -           needs-refinement  Missing acceptance criteria        │
│  #112  -           pending           In analysis queue                  │
└─────────────────────────────────────────────────────────────────────────┘
```

### Keybindings

| Key | Context | Action |
|-----|---------|--------|
| `i` | Task detail | Open source GitHub issue in browser |
| `I` | Global | Switch to Issues tab (if implemented) |

## Dependencies

**Issue-based tasks can depend on module tasks, but not vice versa.**

- Issues often build on existing infrastructure defined in module plans
- Keeps module plans as the "core roadmap" unaffected by incoming issues
- The LLM analyzer detects and declares these dependencies when generating the plan

Example frontmatter:
```yaml
depends_on:
  - billing/E03  # needs invoice model from module plan
```

## Future Enhancements

- [ ] **Release-awareness:** Tag issues with release version when changes ship (requires release tracking infrastructure)
- [ ] **Scheduled analysis:** Background job runs on interval independent of sync
- [ ] **Webhook-driven analysis:** GitHub webhook triggers analysis when label applied
- [ ] **Re-analysis on issue update:** Detect issue edits and re-evaluate if status is `needs-refinement`
