# Flexible Plan Grouping Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Simplify plan parsing to treat any directory with `epic-*.md` files as a group, removing the `-module` suffix preference.

**Architecture:** The `TaskID.Module` field becomes a generic "group" identifier. Directory name = group name, no suffix stripping. Backwards compatible - existing `-module` directories still work, they just keep their full name.

**Tech Stack:** Go, existing parser infrastructure

---

## Task 1: Simplify extractModuleName function

**Files:**
- Modify: `internal/parser/parser.go:310-330`
- Test: `internal/parser/parser_test.go`

**Step 1: Write test for flexible group names**

Add test cases to `TestExtractTaskIDFromPath` to verify various directory patterns work:

```go
// Add these test cases to the existing tests slice
{"docs/plans/billing/epic-00-setup.md", "billing", 0, false},
{"docs/plans/auth-subsystem/epic-01-login.md", "auth-subsystem", 1, false},
{"docs/plans/payment-feature/epic-02-checkout.md", "payment-feature", 2, false},
{"docs/plans/api-v2-migration/epic-00-prep.md", "api-v2-migration", 0, false},
{"docs/plans/technical-module/epic-05-validators.md", "technical-module", 5, false},
```

**Step 2: Run test to verify current behavior**

Run: `go test -run TestExtractTaskIDFromPath ./internal/parser/ -v`

Expected: Some tests fail because current code strips `-module` suffix.

**Step 3: Simplify extractModuleName**

Replace the function with a simpler version that just validates and returns the directory name:

```go
// extractModuleName extracts the group name from a directory name.
// Any valid directory name (lowercase letters, numbers, hyphens) is accepted.
// The directory name IS the group name - no suffix stripping.
func extractModuleName(dirName string) string {
	// Accept any directory with lowercase letters, numbers, and hyphens
	if regexp.MustCompile(`^[a-z][a-z0-9-]*$`).MatchString(dirName) {
		return dirName
	}
	return ""
}
```

**Step 4: Remove unused regex variables**

Delete `moduleDirRegex` and `extendedModuleDirRegex` from lines 19-22.

**Step 5: Run tests to verify**

Run: `go test -run TestExtractTaskIDFromPath ./internal/parser/ -v`

Expected: All tests pass.

**Step 6: Commit**

```bash
git add internal/parser/parser.go internal/parser/parser_test.go
git commit -m "refactor(parser): simplify group name extraction

Remove -module suffix stripping. Directory name is now used as-is
for the group identifier. This enables flexible grouping like:
- billing/ (simple name)
- auth-subsystem/ (subsystem)
- payment-feature/ (feature)
- api-v2-migration/ (migration)

BREAKING: 'technical-module' now becomes group 'technical-module'
instead of 'technical'. Existing plans may need directory renames."
```

---

## Task 2: Update existing tests to use new naming

**Files:**
- Modify: `internal/parser/parser_test.go`

**Step 1: Update TestParseEpicFile**

Change directory from `technical-module` to `technical` and update expected ID:

```go
// In TestParseEpicFile, change:
epicPath := filepath.Join(dir, "technical", "epic-05-validators.md")

// And update assertion:
if task.ID.String() != "technical/E05" {
    t.Errorf("ID = %q, want technical/E05", task.ID.String())
}
```

**Step 2: Update TestParseModuleDir**

Change directory name:

```go
// Change:
moduleDir := filepath.Join(dir, "technical")
```

**Step 3: Update TestParseModuleDir_MissingPredecessor**

Change directory name:

```go
// Change:
moduleDir := filepath.Join(dir, "pm-tool")
```

**Step 4: Run all parser tests**

Run: `go test ./internal/parser/ -v`

Expected: All tests pass.

**Step 5: Commit**

```bash
git add internal/parser/parser_test.go
git commit -m "test(parser): update tests for flexible group naming"
```

---

## Task 3: Simplify ParseReadmeStatuses regex patterns

**Files:**
- Modify: `internal/parser/parser.go:182-275`
- Test: `internal/parser/parser_test.go`

**Step 1: Write test for README status parsing**

```go
func TestParseReadmeStatuses(t *testing.T) {
	dir := t.TempDir()
	plansDir := filepath.Join(dir, "docs", "plans")
	os.MkdirAll(plansDir, 0755)

	// Create README at repo root
	readme := `# Project
| Epic | Description | Status |
|------|-------------|--------|
| [E00](docs/plans/billing/epic-00-setup.md) | Setup | 🟢 |
| [E01](docs/plans/auth-feature/epic-01-login.md) | Login | 🟡 |
| [E02](docs/plans/api-v2/epic-02-endpoints.md) | API | 🔴 |
`
	os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0644)

	statuses := ParseReadmeStatuses(plansDir)

	tests := []struct {
		taskID string
		want   domain.TaskStatus
	}{
		{"billing/E00", domain.StatusComplete},
		{"auth-feature/E01", domain.StatusInProgress},
		{"api-v2/E02", domain.StatusNotStarted},
	}

	for _, tt := range tests {
		if got := statuses[tt.taskID]; got != tt.want {
			t.Errorf("statuses[%q] = %v, want %v", tt.taskID, got, tt.want)
		}
	}
}
```

**Step 2: Run test to see current behavior**

Run: `go test -run TestParseReadmeStatuses ./internal/parser/ -v`

**Step 3: Simplify ParseReadmeStatuses**

Replace the multiple regex patterns with a single flexible pattern:

```go
// ParseReadmeStatuses reads task statuses from traffic light emojis in README.md
func ParseReadmeStatuses(plansDir string) map[string]domain.TaskStatus {
	statuses := make(map[string]domain.TaskStatus)

	// Try README.md at various levels relative to plansDir
	docsDir := filepath.Dir(plansDir)
	repoRoot := filepath.Dir(docsDir)
	readmePaths := []string{
		filepath.Join(repoRoot, "README.md"),
		filepath.Join(docsDir, "README.md"),
		filepath.Join(plansDir, "README.md"),
	}

	var content []byte
	var err error
	for _, p := range readmePaths {
		content, err = os.ReadFile(p)
		if err == nil {
			break
		}
	}
	if err != nil {
		return statuses
	}

	lines := strings.Split(string(content), "\n")

	// Single flexible pattern: | [E##](path/group/epic-##-name.md) | ... | emoji |
	// Captures: group name and epic number from path
	epicPathRegex := regexp.MustCompile(`\|\s*\[E(\d+)\]\([^)]*?/([a-z][a-z0-9-]*)/(?:\d{4}-\d{2}-\d{2}-[a-z0-9-]*-)?epic-(\d+)-[^)]+\.md\)`)

	for _, line := range lines {
		// Skip lines without status emoji
		if !strings.Contains(line, "🔴") && !strings.Contains(line, "🟡") && !strings.Contains(line, "🟢") {
			continue
		}

		matches := epicPathRegex.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		epicNum, _ := strconv.Atoi(matches[3])
		group := matches[2]

		// Extract the emoji
		var status domain.TaskStatus
		if strings.Contains(line, "🟢") {
			status = domain.StatusComplete
		} else if strings.Contains(line, "🟡") {
			status = domain.StatusInProgress
		} else {
			status = domain.StatusNotStarted
		}

		taskID := domain.TaskID{Module: group, EpicNum: epicNum}
		statuses[taskID.String()] = status
	}

	return statuses
}
```

**Step 4: Run tests**

Run: `go test ./internal/parser/ -v`

Expected: All tests pass.

**Step 5: Commit**

```bash
git add internal/parser/parser.go internal/parser/parser_test.go
git commit -m "refactor(parser): simplify README status parsing for flexible groups"
```

---

## Task 4: Update integration test fixtures

**Files:**
- Rename: `integration/fixtures/sample-plans/test-module/` → `integration/fixtures/sample-plans/testing/`
- Rename: `integration/fixtures/sample-plans/billing-module/` → `integration/fixtures/sample-plans/billing/`
- Modify: `integration/fixtures/sample-plans/README.md`

**Step 1: Rename directories**

```bash
mv integration/fixtures/sample-plans/test-module integration/fixtures/sample-plans/testing
mv integration/fixtures/sample-plans/billing-module integration/fixtures/sample-plans/billing
```

**Step 2: Update README.md paths**

Update the README to reference new paths.

**Step 3: Run integration tests**

Run: `go test ./integration/... -v` (if exists) or `go test ./... -v`

**Step 4: Commit**

```bash
git add integration/fixtures/
git commit -m "test(fixtures): rename module directories to simple group names"
```

---

## Task 5: Update documentation

**Files:**
- Modify: `CLAUDE.md`

**Step 1: Update Task Model section**

Change the documentation to reflect flexible grouping:

```markdown
### Task Model

Tasks are identified as `{group}/E{number}` (e.g., `billing/E05`, `auth-feature/E01`).
Groups can represent modules, subsystems, features, or any logical grouping.

Directory structure:
```
docs/plans/
├── billing/              # Simple group
│   ├── epic-00-setup.md
│   └── epic-01-invoicing.md
├── auth-subsystem/       # Subsystem
│   └── epic-00-login.md
├── payment-feature/      # Cross-cutting feature
│   └── epic-00-checkout.md
└── api-v2-migration/     # Migration project
    └── epic-00-prep.md
```

Dependencies are:
- **Implicit**: Within a group, E{N} depends on E{N-1}
- **Explicit**: From frontmatter `depends_on` field
- **Cross-group**: Parsed from frontmatter
```

**Step 2: Update other module references**

Search and replace "module" with "group" where appropriate in CLAUDE.md.

**Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update terminology from module to group"
```

---

## Task 6: Run full test suite

**Step 1: Run all tests**

Run: `go test ./... -v`

Expected: All tests pass.

**Step 2: Build and verify**

Run: `go build -o claude-orch ./cmd/claude-orch`

Expected: Build succeeds.
