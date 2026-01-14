package parser

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
)

var (
	epicFileRegex = regexp.MustCompile(`^epic-(\d+)-.*\.md$`)
	// moduleDirRegex matches standard module directories like "technical-module"
	moduleDirRegex = regexp.MustCompile(`^([a-z][a-z0-9-]*)-module$`)
	// extendedModuleDirRegex matches extended module directories like "customer-module-expanded"
	extendedModuleDirRegex = regexp.MustCompile(`^([a-z][a-z0-9-]*-module-[a-z][a-z0-9-]*)$`)
	titleRegex             = regexp.MustCompile(`^#\s+(.+)$`)
	// Match table rows like: | [E01](path) | Description | 🟢 | or | E01 | Title | 🟢 |
	readmeStatusRegex = regexp.MustCompile(`\|\s*\[?E(\d+)\]?(?:\([^)]*\))?\s*\|.*([🔴🟡🟢])\s*\|`)
)

// ParseEpicFile parses a single epic markdown file into a Task
func ParseEpicFile(path string) (*domain.Task, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	taskID, err := ExtractTaskIDFromPath(path)
	if err != nil {
		return nil, err
	}

	fm, body, err := ParseFrontmatter(content)
	if err != nil {
		return nil, fmt.Errorf("parsing frontmatter: %w", err)
	}

	title := extractTitle(body)
	description := extractDescription(body)
	testSummary := ExtractTestSummary(content)

	deps, err := ParseDependencies(fm.DependsOn)
	if err != nil {
		return nil, fmt.Errorf("parsing dependencies: %w", err)
	}

	now := time.Now()
	return &domain.Task{
		ID:          taskID,
		Title:       title,
		Description: description,
		Status:      ToStatus(fm.Status),
		Priority:    ToPriority(fm.Priority),
		DependsOn:   deps,
		NeedsReview: fm.NeedsReview,
		FilePath:    path,
		TestSummary: testSummary,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// ParseModuleDir parses all epic files in a module directory
func ParseModuleDir(dir string) ([]*domain.Task, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var tasks []*domain.Task
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !epicFileRegex.MatchString(entry.Name()) {
			continue
		}

		task, err := ParseEpicFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", entry.Name(), err)
		}
		tasks = append(tasks, task)
	}

	// Build set of existing task IDs
	existingTasks := make(map[string]bool)
	for _, task := range tasks {
		existingTasks[task.ID.String()] = true
	}

	// Add implicit dependencies (only if target task exists)
	for _, task := range tasks {
		if dep := task.ImplicitDependency(); dep != nil {
			// Only add if the dependency actually exists
			if !existingTasks[dep.String()] {
				continue
			}
			// Check if already in explicit deps
			found := false
			for _, d := range task.DependsOn {
				if d.String() == dep.String() {
					found = true
					break
				}
			}
			if !found {
				task.DependsOn = append(task.DependsOn, *dep)
			}
		}
	}

	return tasks, nil
}

// ParsePlansDir parses all modules in a docs/plans directory
func ParsePlansDir(plansDir string) ([]*domain.Task, error) {
	entries, err := os.ReadDir(plansDir)
	if err != nil {
		return nil, err
	}

	var allTasks []*domain.Task
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Check if directory contains any epic files
		dirPath := filepath.Join(plansDir, entry.Name())
		hasEpics, err := directoryHasEpicFiles(dirPath)
		if err != nil || !hasEpics {
			continue
		}

		tasks, err := ParseModuleDir(dirPath)
		if err != nil {
			// Log but don't fail - some directories might have different structures
			continue
		}
		allTasks = append(allTasks, tasks...)
	}

	// Try to read statuses from README.md
	readmeStatuses := ParseReadmeStatuses(plansDir)
	if len(readmeStatuses) > 0 {
		for _, task := range allTasks {
			// Only override if task doesn't have explicit status in frontmatter
			// (i.e., it's still NotStarted which is the default)
			if task.Status == domain.StatusNotStarted {
				if status, ok := readmeStatuses[task.ID.String()]; ok {
					task.Status = status
				}
			}
		}
	}

	return allTasks, nil
}

// directoryHasEpicFiles checks if a directory contains any epic-*.md files
func directoryHasEpicFiles(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if !entry.IsDir() && epicFileRegex.MatchString(entry.Name()) {
			return true, nil
		}
	}
	return false, nil
}

// ParseReadmeStatuses reads task statuses from traffic light emojis in README.md
func ParseReadmeStatuses(plansDir string) map[string]domain.TaskStatus {
	statuses := make(map[string]domain.TaskStatus)

	// Try README.md at various levels relative to plansDir
	// plansDir is typically docs/plans, so we need to go up two levels to repo root
	docsDir := filepath.Dir(plansDir)                // docs/
	repoRoot := filepath.Dir(docsDir)                // repo root
	readmePaths := []string{
		filepath.Join(repoRoot, "README.md"),        // repo root (most common)
		filepath.Join(docsDir, "README.md"),         // docs/
		filepath.Join(plansDir, "README.md"),        // docs/plans/
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

	// Patterns to extract module from epic file path and status emoji
	// Pattern 1: | [E01](docs/plans/technical-module/epic-01-xxx.md) | Desc | 🟢 |
	epicStandardRegex := regexp.MustCompile(`\|\s*\[E(\d+)\]\([^)]*?([a-z][a-z0-9-]*)-module/epic-(\d+)-[^)]+\.md\)`)

	// Pattern 2: | [E1](docs/plans/2026-01-05-subledger-epic-1-xxx.md) | Desc | 🟢 |
	// Also matches: | [E00](docs/plans/2026-01-07-task-module-epic-0-xxx.md) | Desc | 🟢 |
	epicDatePrefixRegex := regexp.MustCompile(`\|\s*\[E(\d+)\]\([^)]*?\d{4}-\d{2}-\d{2}-([a-z][a-z0-9-]*(?:-module)?)-epic-(\d+)-[^)]+\.md\)`)

	// Pattern 3: | [E03](docs/plans/task-module/2026-01-07-task-module-epic-3-xxx.md) | Desc | 🟢 |
	// Nested: module directory + date-prefixed file
	epicNestedDateRegex := regexp.MustCompile(`\|\s*\[E(\d+)\]\([^)]*?([a-z][a-z0-9-]*)-module/\d{4}-\d{2}-\d{2}-[a-z][a-z0-9-]*-epic-(\d+)-[^)]+\.md\)`)

	// Pattern 4: | [E1](docs/plans/testing-strategy/epic-01-xxx.md) | Desc | 🟢 |
	// Non-module directories (testing-strategy, workflow-module without standard naming)
	epicNonModuleRegex := regexp.MustCompile(`\|\s*\[E(\d+)\]\([^)]*?([a-z][a-z0-9-]+)/epic-(\d+)-[^)]+\.md\)`)

	for _, line := range lines {
		// Skip lines without status emoji
		if !strings.Contains(line, "🔴") && !strings.Contains(line, "🟡") && !strings.Contains(line, "🟢") {
			continue
		}

		var module string
		var epicNum int

		// Try patterns in order of specificity
		if matches := epicStandardRegex.FindStringSubmatch(line); matches != nil {
			// Standard: xxx-module/epic-NN-... -> module = xxx
			epicNum, _ = strconv.Atoi(matches[3])
			module = matches[2]
		} else if matches := epicNestedDateRegex.FindStringSubmatch(line); matches != nil {
			// Nested date: xxx-module/YYYY-MM-DD-...-epic-N-... -> module = xxx
			epicNum, _ = strconv.Atoi(matches[3])
			module = matches[2]
		} else if matches := epicDatePrefixRegex.FindStringSubmatch(line); matches != nil {
			// Date prefix: YYYY-MM-DD-xxx-epic-N-... or YYYY-MM-DD-xxx-module-epic-N-...
			epicNum, _ = strconv.Atoi(matches[3])
			module = matches[2]
			// Strip -module suffix if present to normalize
			module = strings.TrimSuffix(module, "-module")
		} else if matches := epicNonModuleRegex.FindStringSubmatch(line); matches != nil {
			// Non-module directory: xxx/epic-NN-...
			epicNum, _ = strconv.Atoi(matches[3])
			module = matches[2]
		}

		if module == "" {
			continue
		}

		// Extract the emoji
		var emoji string
		if strings.Contains(line, "🟢") {
			emoji = "🟢"
		} else if strings.Contains(line, "🟡") {
			emoji = "🟡"
		} else if strings.Contains(line, "🔴") {
			emoji = "🔴"
		}

		taskID := domain.TaskID{Module: module, EpicNum: epicNum}
		statuses[taskID.String()] = emojiToStatus(emoji)
	}

	return statuses
}

func emojiToStatus(emoji string) domain.TaskStatus {
	switch emoji {
	case "🟢":
		return domain.StatusComplete
	case "🟡":
		return domain.StatusInProgress
	default:
		return domain.StatusNotStarted
	}
}

// ExtractTaskIDFromPath extracts a TaskID from an epic file path
func ExtractTaskIDFromPath(path string) (domain.TaskID, error) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	// Extract module from directory name
	dirBase := filepath.Base(dir)
	module := extractModuleName(dirBase)
	if module == "" {
		return domain.TaskID{}, fmt.Errorf("invalid module directory: %s", dirBase)
	}

	// Extract epic number from filename
	matches := epicFileRegex.FindStringSubmatch(base)
	if matches == nil {
		return domain.TaskID{}, fmt.Errorf("invalid epic filename: %s", base)
	}
	epicNum, _ := strconv.Atoi(matches[1])

	return domain.TaskID{Module: module, EpicNum: epicNum}, nil
}

// extractModuleName extracts the module name from a directory name
// Handles patterns like:
//   - "technical-module" -> "technical"
//   - "customer-module-expanded" -> "customer-module-expanded"
//   - "testing-strategy" -> "testing-strategy"
func extractModuleName(dirName string) string {
	// Try standard module pattern first: xxx-module -> xxx
	if matches := moduleDirRegex.FindStringSubmatch(dirName); matches != nil {
		return matches[1]
	}
	// Try extended module pattern: xxx-module-yyy -> xxx-module-yyy (keep as-is)
	if extendedModuleDirRegex.MatchString(dirName) {
		return dirName
	}
	// For any other directory with lowercase letters and hyphens, use as-is
	// This handles directories like "testing-strategy", "infrastructure", etc.
	if regexp.MustCompile(`^[a-z][a-z0-9-]*$`).MatchString(dirName) {
		return dirName
	}
	return ""
}

func extractTitle(content []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if matches := titleRegex.FindStringSubmatch(line); matches != nil {
			return strings.TrimSpace(matches[1])
		}
	}
	return ""
}

func extractDescription(content []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	var lines []string
	foundTitle := false

	for scanner.Scan() {
		line := scanner.Text()
		if !foundTitle {
			if titleRegex.MatchString(line) {
				foundTitle = true
			}
			continue
		}

		// Skip empty lines immediately after title
		if len(lines) == 0 && strings.TrimSpace(line) == "" {
			continue
		}

		// Stop at next heading
		if strings.HasPrefix(line, "#") {
			break
		}

		lines = append(lines, line)
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// ExtractTestSummary parses the "## Test Summary" section from epic content
func ExtractTestSummary(content []byte) *domain.TestSummary {
	lines := strings.Split(string(content), "\n")

	// Find "## Test Summary" section
	inTestSummary := false
	inTable := false
	inFilesList := false

	summary := &domain.TestSummary{}
	hasData := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check for Test Summary header
		if strings.HasPrefix(trimmed, "## Test Summary") || strings.HasPrefix(trimmed, "## Test Results") {
			inTestSummary = true
			continue
		}

		// Exit on next section
		if inTestSummary && strings.HasPrefix(trimmed, "## ") {
			break
		}

		if !inTestSummary {
			continue
		}

		// Check for table header
		if strings.Contains(trimmed, "| Metric") || strings.Contains(trimmed, "|---") {
			inTable = true
			continue
		}

		// Check for files list
		if strings.Contains(strings.ToLower(trimmed), "files tested") {
			inFilesList = true
			inTable = false
			continue
		}

		// Parse table rows
		if inTable && strings.HasPrefix(trimmed, "|") {
			parts := strings.Split(trimmed, "|")
			if len(parts) >= 3 {
				metric := strings.TrimSpace(parts[1])
				value := strings.TrimSpace(parts[2])

				switch strings.ToLower(metric) {
				case "tests", "total":
					summary.Tests, _ = strconv.Atoi(value)
					hasData = true
				case "passed":
					summary.Passed, _ = strconv.Atoi(value)
					hasData = true
				case "failed":
					summary.Failed, _ = strconv.Atoi(value)
					hasData = true
				case "skipped":
					summary.Skipped, _ = strconv.Atoi(value)
					hasData = true
				case "coverage":
					summary.Coverage = value
					hasData = true
				}
			}
		}

		// Parse files list
		if inFilesList && strings.HasPrefix(trimmed, "- ") {
			file := strings.TrimPrefix(trimmed, "- ")
			summary.FilesTested = append(summary.FilesTested, file)
			hasData = true
		}
	}

	if !hasData {
		return nil
	}
	return summary
}
