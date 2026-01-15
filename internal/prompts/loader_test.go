package prompts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoaderLoadEmbedded(t *testing.T) {
	loader := NewLoader() // No override dirs

	// Test loading epic template
	tmpl, meta, err := loader.LoadTemplate("epic/task.md")
	if err != nil {
		t.Fatalf("failed to load epic template: %v", err)
	}
	if tmpl == nil {
		t.Fatal("template should not be nil")
	}
	if meta != nil {
		t.Fatal("epic template should not have frontmatter metadata")
	}
}

func TestLoaderLoadMaintenanceWithFrontmatter(t *testing.T) {
	loader := NewLoader()

	tmpl, meta, err := loader.LoadTemplate("maintenance/refactor.md")
	if err != nil {
		t.Fatalf("failed to load maintenance template: %v", err)
	}
	if tmpl == nil {
		t.Fatal("template should not be nil")
	}
	if meta == nil {
		t.Fatal("maintenance template should have frontmatter metadata")
	}
	if meta.ID != "refactor" {
		t.Errorf("expected ID 'refactor', got '%s'", meta.ID)
	}
	if meta.Name != "Refactor Code" {
		t.Errorf("expected Name 'Refactor Code', got '%s'", meta.Name)
	}
	if len(meta.Scopes) != 3 {
		t.Errorf("expected 3 scopes, got %d", len(meta.Scopes))
	}
}

func TestLoaderOverride(t *testing.T) {
	// Create temp directory for overrides
	tmpDir, err := os.MkdirTemp("", "prompts-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create override directory structure
	epicDir := filepath.Join(tmpDir, "epic")
	if err := os.MkdirAll(epicDir, 0755); err != nil {
		t.Fatalf("failed to create epic dir: %v", err)
	}

	// Write custom override
	customContent := `You are implementing a CUSTOM task: {{.Title}}

This is a custom override prompt for testing.

Epic: {{.EpicFilePath}}
{{.EpicContent}}
`
	if err := os.WriteFile(filepath.Join(epicDir, "task.md"), []byte(customContent), 0644); err != nil {
		t.Fatalf("failed to write override file: %v", err)
	}

	// Create loader with override dir
	loader := NewLoader(tmpDir)

	// Execute template
	result, err := loader.BuildEpicPrompt(EpicData{
		Title:        "Test Feature",
		EpicFilePath: "test/E01",
		EpicContent:  "Implement something",
	})
	if err != nil {
		t.Fatalf("failed to build epic prompt: %v", err)
	}

	// Verify override was used
	if !strings.Contains(result, "CUSTOM task") {
		t.Errorf("override was not used, got: %s", result)
	}
	if !strings.Contains(result, "Test Feature") {
		t.Errorf("template substitution failed, got: %s", result)
	}
}

func TestLoaderOverridePrecedence(t *testing.T) {
	// Create two temp directories for testing precedence
	projectDir, err := os.MkdirTemp("", "prompts-project-*")
	if err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}
	defer os.RemoveAll(projectDir)

	userDir, err := os.MkdirTemp("", "prompts-user-*")
	if err != nil {
		t.Fatalf("failed to create user dir: %v", err)
	}
	defer os.RemoveAll(userDir)

	// Create epic dirs in both
	for _, dir := range []string{projectDir, userDir} {
		if err := os.MkdirAll(filepath.Join(dir, "epic"), 0755); err != nil {
			t.Fatalf("failed to create epic dir: %v", err)
		}
	}

	// Write different overrides to each
	projectContent := `PROJECT OVERRIDE: {{.Title}}`
	userContent := `USER OVERRIDE: {{.Title}}`

	if err := os.WriteFile(filepath.Join(projectDir, "epic", "task.md"), []byte(projectContent), 0644); err != nil {
		t.Fatalf("failed to write project override: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "epic", "task.md"), []byte(userContent), 0644); err != nil {
		t.Fatalf("failed to write user override: %v", err)
	}

	// Create loader with project dir first (higher priority)
	loader := NewLoader(projectDir, userDir)

	result, err := loader.BuildEpicPrompt(EpicData{Title: "Test"})
	if err != nil {
		t.Fatalf("failed to build prompt: %v", err)
	}

	// Project override should take precedence
	if !strings.Contains(result, "PROJECT OVERRIDE") {
		t.Errorf("project override should take precedence, got: %s", result)
	}
}

func TestLoaderFallbackToEmbedded(t *testing.T) {
	// Create empty temp directory (no overrides)
	tmpDir, err := os.MkdirTemp("", "prompts-empty-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create loader with empty override dir
	loader := NewLoader(tmpDir)

	// Should fall back to embedded template
	result, err := loader.BuildEpicPrompt(EpicData{
		Title:         "Test Feature",
		EpicFilePath:  "test/E01",
		EpicContent:   "Content here",
		CompletedDeps: "None",
	})
	if err != nil {
		t.Fatalf("failed to build prompt: %v", err)
	}

	// Verify embedded content is used (check for expected text from embedded template)
	if !strings.Contains(result, "autonomous-plan-execution") {
		t.Errorf("should fall back to embedded template, got: %s", result)
	}
}

func TestLoaderMaintenanceOverride(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "prompts-maint-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create maintenance override dir
	maintDir := filepath.Join(tmpDir, "maintenance")
	if err := os.MkdirAll(maintDir, 0755); err != nil {
		t.Fatalf("failed to create maintenance dir: %v", err)
	}

	// Write custom refactor template with frontmatter
	customContent := `---
id: refactor
name: Custom Refactor
description: My custom refactoring task
scopes: [module, all]
---
CUSTOM REFACTOR on {{.Scope}}

Do custom refactoring things.
`
	if err := os.WriteFile(filepath.Join(maintDir, "refactor.md"), []byte(customContent), 0644); err != nil {
		t.Fatalf("failed to write override: %v", err)
	}

	loader := NewLoader(tmpDir)

	// Load template and check metadata
	_, meta, err := loader.LoadTemplate("maintenance/refactor.md")
	if err != nil {
		t.Fatalf("failed to load template: %v", err)
	}

	if meta.Name != "Custom Refactor" {
		t.Errorf("expected 'Custom Refactor', got '%s'", meta.Name)
	}
	if len(meta.Scopes) != 2 {
		t.Errorf("expected 2 scopes, got %d", len(meta.Scopes))
	}
}

func TestLoaderSkillOverride(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "prompts-skill-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create skills override dir
	skillsDir := filepath.Join(tmpDir, "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatalf("failed to create skills dir: %v", err)
	}

	// Write custom skill
	customSkill := `---
name: autonomous-plan-execution
description: Custom autonomous execution
---

# Custom Autonomous Execution

This is a customized skill for testing.
`
	if err := os.WriteFile(filepath.Join(skillsDir, "autonomous-plan-execution.md"), []byte(customSkill), 0644); err != nil {
		t.Fatalf("failed to write skill override: %v", err)
	}

	loader := NewLoader(tmpDir)

	content, err := loader.GetSkillContent()
	if err != nil {
		t.Fatalf("failed to get skill content: %v", err)
	}

	if !strings.Contains(content, "Custom Autonomous Execution") {
		t.Errorf("skill override was not used, got: %s", content)
	}
}

func TestLoaderListMaintenanceTemplates(t *testing.T) {
	loader := NewLoader()

	metas, err := loader.ListMaintenanceTemplates()
	if err != nil {
		t.Fatalf("failed to list templates: %v", err)
	}

	// Should have at least the built-in templates (refactor, cleanup, optimize, docs, tests, security, lint)
	if len(metas) < 7 {
		t.Errorf("expected at least 7 maintenance templates, got %d", len(metas))
	}

	// Verify one of them
	found := false
	for _, m := range metas {
		if m.ID == "security" {
			found = true
			if m.Name != "Security Review" {
				t.Errorf("expected 'Security Review', got '%s'", m.Name)
			}
			break
		}
	}
	if !found {
		t.Error("security template not found in list")
	}
}

func TestLoaderCaching(t *testing.T) {
	loader := NewLoader()

	// Load same template twice
	tmpl1, _, err := loader.LoadTemplate("epic/task.md")
	if err != nil {
		t.Fatalf("first load failed: %v", err)
	}

	tmpl2, _, err := loader.LoadTemplate("epic/task.md")
	if err != nil {
		t.Fatalf("second load failed: %v", err)
	}

	// Should be the same pointer (cached)
	if tmpl1 != tmpl2 {
		t.Error("template should be cached and return same pointer")
	}

	// Clear cache and reload
	loader.ClearCache()

	tmpl3, _, err := loader.LoadTemplate("epic/task.md")
	if err != nil {
		t.Fatalf("third load failed: %v", err)
	}

	// Should be a different pointer after cache clear
	if tmpl1 == tmpl3 {
		t.Error("template should be reloaded after cache clear")
	}
}

func TestEpicTemplateExecution(t *testing.T) {
	loader := NewLoader()

	data := EpicData{
		Title:         "Implement User Auth",
		EpicFilePath:  "auth/E01",
		EpicContent:   "Create login and logout endpoints",
		ModuleContext: "Auth module handles all authentication",
		CompletedDeps: "core/E01, core/E02",
	}

	result, err := loader.BuildEpicPrompt(data)
	if err != nil {
		t.Fatalf("failed to build prompt: %v", err)
	}

	// Verify all data is substituted
	checks := []string{
		"Implement User Auth",
		"auth/E01",
		"Create login and logout endpoints",
		"Auth module handles all authentication",
		"core/E01, core/E02",
	}

	for _, check := range checks {
		if !strings.Contains(result, check) {
			t.Errorf("expected result to contain '%s'", check)
		}
	}
}

func TestMaintenanceTemplateExecution(t *testing.T) {
	loader := NewLoader()

	data := MaintenanceData{
		Prompt: "Refactor the code in the 'api' module",
		Scope:  "the 'api' module",
		Module: "api",
	}

	result, err := loader.BuildMaintenancePrompt(data)
	if err != nil {
		t.Fatalf("failed to build prompt: %v", err)
	}

	// Verify prompt is included and wrapper is applied
	if !strings.Contains(result, "Refactor the code") {
		t.Error("prompt content should be included")
	}
	if !strings.Contains(result, "AUTONOMOUS EXECUTION") {
		t.Error("autonomous wrapper should be applied")
	}
}
