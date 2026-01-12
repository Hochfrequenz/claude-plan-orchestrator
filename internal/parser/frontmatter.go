package parser

import (
	"bytes"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
	"gopkg.in/yaml.v3"
)

// Frontmatter represents the YAML frontmatter in epic files
type Frontmatter struct {
	Priority    string   `yaml:"priority"`
	DependsOn   []string `yaml:"depends_on"`
	NeedsReview bool     `yaml:"needs_review"`
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
	result := make([]domain.TaskID, 0, len(deps))
	for _, d := range deps {
		tid, err := domain.ParseTaskID(d)
		if err != nil {
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
	case "low":
		return domain.PriorityLow
	default:
		return domain.PriorityNormal
	}
}
