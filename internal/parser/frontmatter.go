package parser

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
	"gopkg.in/yaml.v3"
)

// Frontmatter represents the YAML frontmatter in epic files
type Frontmatter struct {
	Status      string   `yaml:"status"`
	Priority    string   `yaml:"priority"`
	DependsOn   []string `yaml:"depends_on"`
	NeedsReview bool     `yaml:"needs_review"`
	GitHubIssue *int     `yaml:"github_issue"`
}

// ParseFrontmatter extracts YAML frontmatter from markdown content
// Returns the frontmatter, remaining content, and any error
func ParseFrontmatter(content []byte) (*Frontmatter, []byte, error) {
	if !bytes.HasPrefix(content, []byte("---\n")) {
		return &Frontmatter{}, content, nil
	}

	// Find end of frontmatter
	rest := content[4:]
	endIdx := bytes.Index(rest, []byte("\n---"))
	if endIdx == -1 {
		return &Frontmatter{}, content, nil
	}

	fmData := rest[:endIdx]
	remaining := rest[endIdx+4:] // skip \n---

	var fm Frontmatter
	if err := yaml.Unmarshal(fmData, &fm); err != nil {
		return nil, nil, err
	}

	return &fm, bytes.TrimLeft(remaining, "\n"), nil
}

// ParseDependencies converts string dependency IDs to TaskIDs
func ParseDependencies(deps []string) ([]domain.TaskID, error) {
	return ParseDependenciesInModule(deps, "")
}

// ParseDependenciesInModule converts string dependency IDs to TaskIDs.
// Bare numbers (e.g. "1", "03") are resolved as E{num} within the given module.
func ParseDependenciesInModule(deps []string, module string) ([]domain.TaskID, error) {
	result := make([]domain.TaskID, 0, len(deps))
	for _, d := range deps {
		tid, err := domain.ParseTaskID(d)
		if err != nil {
			// Try bare number: resolve as E{num} in the same module
			if module != "" {
				num, numErr := strconv.Atoi(strings.TrimSpace(d))
				if numErr == nil {
					result = append(result, domain.TaskID{Module: module, EpicNum: num})
					continue
				}
			}
			return nil, err
		}
		result = append(result, tid)
	}
	return result, nil
}

// ToPriority converts a string to a Priority
func ToPriority(s string) domain.Priority {
	switch s {
	case "high":
		return domain.PriorityHigh
	case "medium":
		return domain.PriorityMedium
	case "low":
		return domain.PriorityLow
	default:
		return domain.PriorityNormal
	}
}

// ToStatus converts a string to a TaskStatus
func ToStatus(s string) domain.TaskStatus {
	switch s {
	case "in_progress", "inprogress", "in-progress", "running":
		return domain.StatusInProgress
	case "complete", "completed", "done":
		return domain.StatusComplete
	default:
		return domain.StatusNotStarted
	}
}
