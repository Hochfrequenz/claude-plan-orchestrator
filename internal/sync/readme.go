package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
)

// GenerateStatusTable generates a markdown status table for a module
func GenerateStatusTable(tasks []*domain.Task) string {
	var b strings.Builder

	b.WriteString("| Epic | Status | Title |\n")
	b.WriteString("|------|--------|-------|\n")

	for _, task := range tasks {
		emoji := StatusEmoji(task.Status)
		b.WriteString(fmt.Sprintf("| E%02d | %s | %s |\n",
			task.ID.EpicNum, emoji, task.Title))
	}

	return b.String()
}

// EnsureREADME creates a README.md if it doesn't exist
func (s *Syncer) EnsureREADME() error {
	readmePath := filepath.Join(s.plansDir, "README.md")
	if _, err := os.Stat(readmePath); err == nil {
		return nil // Already exists
	}

	content := `# EnergyERP Development Plans

This directory contains implementation plans organized by module.

## Status Legend

- 🔴 Not started
- 🟡 In progress
- 🟢 Complete

---

`
	return os.WriteFile(readmePath, []byte(content), 0644)
}

// titleCase converts the first letter to uppercase
func titleCase(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// AppendModuleSection adds a module section to README
func (s *Syncer) AppendModuleSection(module string, tasks []*domain.Task) error {
	readmePath := filepath.Join(s.plansDir, "README.md")
	content, err := os.ReadFile(readmePath)
	if err != nil {
		return err
	}

	// Check if module section exists
	sectionHeader := fmt.Sprintf("## %s Module", titleCase(module))
	if strings.Contains(string(content), sectionHeader) {
		return nil // Already exists
	}

	// Append new section
	var b strings.Builder
	b.Write(content)
	b.WriteString(fmt.Sprintf("\n%s\n\n", sectionHeader))
	b.WriteString(GenerateStatusTable(tasks))
	b.WriteString("\n")

	return os.WriteFile(readmePath, []byte(b.String()), 0644)
}
