package skills

import (
	"os"
	"path/filepath"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/prompts"
)

// skillLoader is the loader used for skill content.
var skillLoader = prompts.GetDefaultLoader()

// SetSkillLoader allows overriding the skill loader (for testing or custom config).
func SetSkillLoader(loader *prompts.Loader) {
	skillLoader = loader
}

// EnsureInstalled checks if required skills are installed and creates them if missing.
// Returns true if any skills were installed.
func EnsureInstalled() (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}

	skillDir := filepath.Join(home, ".claude", "skills", "autonomous-plan-execution")
	skillFile := filepath.Join(skillDir, "SKILL.md")

	// Check if already exists
	if _, err := os.Stat(skillFile); err == nil {
		return false, nil // Already installed
	}

	// Load skill content from prompts (supports overrides)
	content, err := skillLoader.GetSkillContent()
	if err != nil {
		return false, err
	}

	// Create directory
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return false, err
	}

	// Write skill file
	if err := os.WriteFile(skillFile, []byte(content), 0644); err != nil {
		return false, err
	}

	return true, nil
}

// GetSkillPath returns the path where the skill is installed
func GetSkillPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "skills", "autonomous-plan-execution", "SKILL.md")
}
