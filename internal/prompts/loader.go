package prompts

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"

	"gopkg.in/yaml.v3"
)

// Loader manages prompt templates with override support.
type Loader struct {
	overrideDirs []string // Directories to check for overrides (in priority order)
	cache        map[string]*template.Template
	metaCache    map[string]*TemplateMeta
	mu           sync.RWMutex
}

// TemplateMeta holds frontmatter metadata for maintenance templates.
type TemplateMeta struct {
	ID          string   `yaml:"id"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Scopes      []string `yaml:"scopes"`
}

// NewLoader creates a loader with the given override directories.
// Directories are checked in order; first match wins.
func NewLoader(overrideDirs ...string) *Loader {
	return &Loader{
		overrideDirs: overrideDirs,
		cache:        make(map[string]*template.Template),
		metaCache:    make(map[string]*TemplateMeta),
	}
}

// DefaultLoader creates a loader with standard override paths:
// 1. Project-local: .claude-orchestrator/prompts/
// 2. User config: ~/.config/claude-orchestrator/prompts/
func DefaultLoader(projectRoot string) *Loader {
	home, _ := os.UserHomeDir()
	dirs := []string{}

	if projectRoot != "" {
		dirs = append(dirs, filepath.Join(projectRoot, ".claude-orchestrator", "prompts"))
	}
	dirs = append(dirs, filepath.Join(home, ".config", "claude-orchestrator", "prompts"))

	return NewLoader(dirs...)
}

// loadContent loads raw content from override dirs or embedded FS.
func (l *Loader) loadContent(path string) ([]byte, error) {
	// Check override directories first
	for _, dir := range l.overrideDirs {
		fullPath := filepath.Join(dir, path)
		if data, err := os.ReadFile(fullPath); err == nil {
			return data, nil
		}
	}

	// Fall back to embedded
	return fs.ReadFile(embeddedFS, path)
}

// parseFrontmatter splits content into frontmatter and body.
func parseFrontmatter(content []byte) (*TemplateMeta, string, error) {
	str := string(content)

	// Check for frontmatter delimiter
	if !strings.HasPrefix(str, "---\n") {
		return nil, str, nil // No frontmatter
	}

	// Find closing delimiter
	end := strings.Index(str[4:], "\n---\n")
	if end == -1 {
		return nil, str, nil // Malformed, treat as no frontmatter
	}

	frontmatter := str[4 : 4+end]
	body := str[4+end+5:] // Skip closing "---\n"

	var meta TemplateMeta
	if err := yaml.Unmarshal([]byte(frontmatter), &meta); err != nil {
		return nil, "", fmt.Errorf("parse frontmatter: %w", err)
	}

	return &meta, body, nil
}

// LoadTemplate loads and parses a template by path (e.g., "epic/task.md").
func (l *Loader) LoadTemplate(path string) (*template.Template, *TemplateMeta, error) {
	l.mu.RLock()
	if tmpl, ok := l.cache[path]; ok {
		meta := l.metaCache[path]
		l.mu.RUnlock()
		return tmpl, meta, nil
	}
	l.mu.RUnlock()

	content, err := l.loadContent(path)
	if err != nil {
		return nil, nil, fmt.Errorf("load %s: %w", path, err)
	}

	meta, body, err := parseFrontmatter(content)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}

	tmpl, err := template.New(path).Parse(body)
	if err != nil {
		return nil, nil, fmt.Errorf("compile template %s: %w", path, err)
	}

	l.mu.Lock()
	l.cache[path] = tmpl
	l.metaCache[path] = meta
	l.mu.Unlock()

	return tmpl, meta, nil
}

// LoadRaw loads raw content without template parsing (for skills).
func (l *Loader) LoadRaw(path string) (string, error) {
	content, err := l.loadContent(path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// Execute loads and executes a template with the given data.
func (l *Loader) Execute(path string, data interface{}) (string, error) {
	tmpl, _, err := l.LoadTemplate(path)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute %s: %w", path, err)
	}

	return buf.String(), nil
}

// ListMaintenanceTemplates returns metadata for all maintenance templates.
func (l *Loader) ListMaintenanceTemplates() ([]*TemplateMeta, error) {
	// Get list from embedded FS
	entries, err := fs.ReadDir(embeddedFS, "maintenance")
	if err != nil {
		return nil, err
	}

	var result []*TemplateMeta
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		// Skip wrapper template
		if entry.Name() == "wrapper.md" {
			continue
		}

		path := filepath.Join("maintenance", entry.Name())
		_, meta, err := l.LoadTemplate(path)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", path, err)
		}
		if meta != nil {
			result = append(result, meta)
		}
	}

	return result, nil
}

// GetMaintenanceTemplate returns metadata and content for a maintenance template by ID.
func (l *Loader) GetMaintenanceTemplate(id string) (*TemplateMeta, string, error) {
	path := filepath.Join("maintenance", id+".md")
	tmpl, meta, err := l.LoadTemplate(path)
	if err != nil {
		return nil, "", err
	}

	// Execute with empty data to get raw template body
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct{}{}); err != nil {
		return nil, "", err
	}

	return meta, buf.String(), nil
}

// EpicData holds template variables for epic prompts.
type EpicData struct {
	Title         string
	EpicFilePath  string
	EpicContent   string
	ModuleContext string
	CompletedDeps string
}

// MaintenanceData holds template variables for maintenance prompts.
type MaintenanceData struct {
	Prompt string
	Scope  string
	Module string
}

// BuildEpicPrompt loads and executes the epic prompt template.
func (l *Loader) BuildEpicPrompt(data EpicData) (string, error) {
	return l.Execute("epic/task.md", data)
}

// BuildMaintenancePrompt loads and executes the maintenance wrapper template.
func (l *Loader) BuildMaintenancePrompt(data MaintenanceData) (string, error) {
	return l.Execute("maintenance/wrapper.md", data)
}

// GetSkillContent returns the content of the autonomous-plan-execution skill.
func (l *Loader) GetSkillContent() (string, error) {
	return l.LoadRaw("skills/autonomous-plan-execution.md")
}

// ClearCache clears the template cache (useful for development/testing).
func (l *Loader) ClearCache() {
	l.mu.Lock()
	l.cache = make(map[string]*template.Template)
	l.metaCache = make(map[string]*TemplateMeta)
	l.mu.Unlock()
}

// Global default loader (initialized lazily)
var (
	defaultLoader     *Loader
	defaultLoaderOnce sync.Once
)

// GetDefaultLoader returns the global default loader.
func GetDefaultLoader() *Loader {
	defaultLoaderOnce.Do(func() {
		defaultLoader = DefaultLoader("")
	})
	return defaultLoader
}

// SetDefaultLoader allows overriding the default loader (for testing or custom config).
func SetDefaultLoader(loader *Loader) {
	defaultLoader = loader
}
