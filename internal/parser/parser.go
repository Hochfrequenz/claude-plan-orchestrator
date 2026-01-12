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
	epicFileRegex  = regexp.MustCompile(`^epic-(\d+)-.*\.md$`)
	moduleDirRegex = regexp.MustCompile(`^([a-z][a-z0-9-]*)-module$`)
	titleRegex     = regexp.MustCompile(`^#\s+(.+)$`)
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

	// Add implicit dependencies
	for _, task := range tasks {
		if dep := task.ImplicitDependency(); dep != nil {
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
		if !moduleDirRegex.MatchString(entry.Name()) {
			continue
		}

		tasks, err := ParseModuleDir(filepath.Join(plansDir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("parsing module %s: %w", entry.Name(), err)
		}
		allTasks = append(allTasks, tasks...)
	}

	return allTasks, nil
}

// ExtractTaskIDFromPath extracts a TaskID from an epic file path
func ExtractTaskIDFromPath(path string) (domain.TaskID, error) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	// Extract module from directory name
	dirBase := filepath.Base(dir)
	matches := moduleDirRegex.FindStringSubmatch(dirBase)
	if matches == nil {
		return domain.TaskID{}, fmt.Errorf("invalid module directory: %s", dirBase)
	}
	module := matches[1]

	// Extract epic number from filename
	matches = epicFileRegex.FindStringSubmatch(base)
	if matches == nil {
		return domain.TaskID{}, fmt.Errorf("invalid epic filename: %s", base)
	}
	epicNum, _ := strconv.Atoi(matches[1])

	return domain.TaskID{Module: module, EpicNum: epicNum}, nil
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
