// Package maintenance provides maintenance task templates for the orchestrator.
package maintenance

import (
	"sync"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/prompts"
)

// Template represents a maintenance task template
type Template struct {
	ID          string   // Unique identifier
	Name        string   // Display name
	Description string   // Short description for the UI
	Prompt      string   // Full prompt template (use {scope} and {module} placeholders)
	ScopeTypes  []string // Supported scopes: "module", "package", "all"
}

// templateLoader is the loader used for template content.
var templateLoader = prompts.GetDefaultLoader()

// SetTemplateLoader allows overriding the template loader (for testing or custom config).
func SetTemplateLoader(loader *prompts.Loader) {
	templateLoader = loader
	// Clear cached templates when loader changes
	builtinOnce = sync.Once{}
	cachedTemplates = nil
}

var (
	builtinOnce     sync.Once
	cachedTemplates []Template
)

// BuiltinTemplates returns the default maintenance task templates.
// Templates are loaded from embedded markdown files with override support.
func BuiltinTemplates() []Template {
	builtinOnce.Do(func() {
		cachedTemplates = loadTemplates()
	})
	return cachedTemplates
}

// loadTemplates loads all maintenance templates from the prompts loader.
func loadTemplates() []Template {
	metas, err := templateLoader.ListMaintenanceTemplates()
	if err != nil {
		// Fallback to empty list on error
		return nil
	}

	templates := make([]Template, 0, len(metas))
	for _, meta := range metas {
		_, prompt, err := templateLoader.GetMaintenanceTemplate(meta.ID)
		if err != nil {
			continue
		}
		templates = append(templates, Template{
			ID:          meta.ID,
			Name:        meta.Name,
			Description: meta.Description,
			Prompt:      prompt,
			ScopeTypes:  meta.Scopes,
		})
	}

	// Always add custom template (no prompt content)
	templates = append(templates, Template{
		ID:          "custom",
		Name:        "Custom Task",
		Description: "Define your own maintenance task",
		Prompt:      "",
		ScopeTypes:  []string{"module", "package", "all"},
	})

	return templates
}

// GetTemplate returns a template by ID, or nil if not found
func GetTemplate(id string) *Template {
	templates := BuiltinTemplates()
	for i := range templates {
		if templates[i].ID == id {
			return &templates[i]
		}
	}
	return nil
}

// ScopeSupported checks if a template supports a given scope type
func (t *Template) ScopeSupported(scope string) bool {
	for _, s := range t.ScopeTypes {
		if s == scope {
			return true
		}
	}
	return false
}
