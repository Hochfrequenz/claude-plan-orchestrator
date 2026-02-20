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
	// Epic file patterns - supports multiple naming conventions:
	// - epic-01-name.md (standard) -> E01
	// - 01-epic-name.md (number prefix) -> E01
	// - epic-cli-02-name.md (epic with subsystem prefix) -> CLI02
	// - epic-1.2-name.md (phase.epic dot notation) -> E2 (uses second number as epic)
	epicFileStandard    = regexp.MustCompile(`^epic-(\d+)-.*\.md$`)          // epic-01-name.md
	epicFileNumPrefix   = regexp.MustCompile(`^(\d+)-epic-.*\.md$`)          // 01-epic-name.md
	epicFileSubsystem   = regexp.MustCompile(`^epic-([a-z]+)-(\d+)-.*\.md$`) // epic-cli-02-name.md
	epicFileDotNotation = regexp.MustCompile(`^epic-(\d+)\.(\d+)-.*\.md$`)   // epic-1.2-name.md

	titleRegex = regexp.MustCompile(`^#\s+(.+)$`)
	// Match table rows like: | [E01](path) | Description | 🟢 | or | E01 | Title | 🟢 |
	readmeStatusRegex = regexp.MustCompile(`\|\s*\[?E(\d+)\]?(?:\([^)]*\))?\s*\|.*([🔴🟡🟢])\s*\|`)
)

// matchEpicFile tries to match a filename against epic file patterns.
// Returns the prefix (uppercase, e.g., "CLI", "TUI", or "" for standard), epic number, and true if matched.
func matchEpicFile(filename string) (prefix string, epicNum int, ok bool) {
	// Standard pattern: epic-01-name.md -> E01
	if matches := epicFileStandard.FindStringSubmatch(filename); matches != nil {
		num, _ := strconv.Atoi(matches[1])
		return "", num, true
	}
	// Number prefix pattern: 01-epic-name.md -> E01
	if matches := epicFileNumPrefix.FindStringSubmatch(filename); matches != nil {
		num, _ := strconv.Atoi(matches[1])
		return "", num, true
	}
	// Subsystem prefix pattern: epic-cli-02-name.md -> CLI02
	if matches := epicFileSubsystem.FindStringSubmatch(filename); matches != nil {
		num, _ := strconv.Atoi(matches[2])
		return strings.ToUpper(matches[1]), num, true
	}
	// Dot notation pattern: epic-1.2-name.md -> E2 (second number is epic within phase)
	if matches := epicFileDotNotation.FindStringSubmatch(filename); matches != nil {
		num, _ := strconv.Atoi(matches[2])
		return "", num, true
	}
	return "", 0, false
}

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

	deps, err := ParseDependenciesInModule(fm.DependsOn, taskID.Module)
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
		GitHubIssue: fm.GitHubIssue,
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
		if _, _, ok := matchEpicFile(entry.Name()); !ok {
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

	// Add implicit dependencies (find highest existing predecessor in same sequence)
	for _, task := range tasks {
		// Find the highest existing task in same sequence (module+prefix) below this one
		var bestDep *domain.TaskID
		for epicNum := task.ID.EpicNum - 1; epicNum >= 0; epicNum-- {
			candidate := domain.TaskID{Module: task.ID.Module, Prefix: task.ID.Prefix, EpicNum: epicNum}
			if existingTasks[candidate.String()] {
				bestDep = &candidate
				break // Found highest existing predecessor
			}
		}

		if bestDep == nil {
			continue // No predecessor exists
		}

		// Check if already in explicit deps
		found := false
		for _, d := range task.DependsOn {
			if d.String() == bestDep.String() {
				found = true
				break
			}
		}
		if !found {
			task.DependsOn = append(task.DependsOn, *bestDep)
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
		if !entry.IsDir() {
			if _, _, ok := matchEpicFile(entry.Name()); ok {
				return true, nil
			}
		}
	}
	return false, nil
}

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

	// Pattern 1: Directory-based paths like group/epic-##-name.md or group/YYYY-MM-DD-...-epic-##-name.md
	// Captures: group name (directory) and epic number
	epicDirRegex := regexp.MustCompile(`\|\s*\[E(\d+)\]\([^)]*?/([a-z][a-z0-9-]*)/(?:\d{4}-\d{2}-\d{2}-[a-z0-9-]*-)?epic-(\d+)-[^)]+\.md\)`)

	// Pattern 2: Date-prefixed files directly in plans/ like YYYY-MM-DD-module-epic-##-name.md
	// Captures: module name from filename and epic number
	epicDatePrefixRegex := regexp.MustCompile(`\|\s*\[E(\d+)\]\([^)]*?/\d{4}-\d{2}-\d{2}-([a-z][a-z0-9-]*)-epic-(\d+)-[^)]+\.md\)`)

	for _, line := range lines {
		// Skip lines without status emoji
		if !strings.Contains(line, "🔴") && !strings.Contains(line, "🟡") && !strings.Contains(line, "🟢") {
			continue
		}

		var group string
		var epicNum int

		// Try date-prefix pattern first (files directly in plans/ without subdirectory)
		// This must come first because the directory pattern would incorrectly match "plans" as the group
		if matches := epicDatePrefixRegex.FindStringSubmatch(line); matches != nil {
			epicNum, _ = strconv.Atoi(matches[3])
			group = matches[2]
		} else if matches := epicDirRegex.FindStringSubmatch(line); matches != nil {
			// Directory-based pattern: group/epic-##-name.md
			epicNum, _ = strconv.Atoi(matches[3])
			group = matches[2]
		}

		if group == "" {
			continue
		}

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

	// Extract prefix and epic number from filename
	prefix, epicNum, ok := matchEpicFile(base)
	if !ok {
		return domain.TaskID{}, fmt.Errorf("invalid epic filename: %s", base)
	}

	return domain.TaskID{Module: module, Prefix: prefix, EpicNum: epicNum}, nil
}

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
