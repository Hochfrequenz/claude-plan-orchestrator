package executor

import (
	"fmt"
	"strings"

	"github.com/hochfrequenz/claude-plan-orchestrator/internal/domain"
	"github.com/hochfrequenz/claude-plan-orchestrator/internal/prompts"
)

// promptLoader is the loader used for prompt templates.
// Can be overridden for testing or custom configuration.
var promptLoader = prompts.GetDefaultLoader()

// SetPromptLoader allows overriding the prompt loader (for testing or custom config).
func SetPromptLoader(loader *prompts.Loader) {
	promptLoader = loader
}

// BuildPrompt constructs the task prompt for Claude Code
func BuildPrompt(task *domain.Task, epicContent, moduleOverview string, completedDeps []string) string {
	depsStr := "None"
	if len(completedDeps) > 0 {
		depsStr = strings.Join(completedDeps, ", ")
	}

	epicFilePath := task.FilePath
	if epicFilePath == "" {
		epicFilePath = fmt.Sprintf("%s/E%02d", task.ID.Module, task.ID.EpicNum)
	}

	data := prompts.EpicData{
		Title:         task.Title,
		EpicFilePath:  epicFilePath,
		EpicContent:   epicContent,
		ModuleContext: moduleOverview,
		CompletedDeps: depsStr,
	}

	result, err := promptLoader.BuildEpicPrompt(data)
	if err != nil {
		// Fallback to a basic prompt if template fails
		return fmt.Sprintf("Implement: %s\n\nEpic: %s\n\n%s", task.Title, epicFilePath, epicContent)
	}
	return result
}

// BuildCommitMessage creates the commit message format
func BuildCommitMessage(task *domain.Task, summary string) string {
	return fmt.Sprintf("feat(%s): implement E%02d - %s\n\n%s",
		task.ID.Module,
		task.ID.EpicNum,
		task.Title,
		summary,
	)
}

// BuildMaintenancePrompt constructs the prompt for a maintenance task
func BuildMaintenancePrompt(templatePrompt, scope, targetModule string) string {
	// Replace {scope} placeholder with the actual scope description
	var scopeDesc string
	switch scope {
	case "module":
		scopeDesc = fmt.Sprintf("the '%s' module", targetModule)
	case "package":
		scopeDesc = fmt.Sprintf("the '%s' package and related packages", targetModule)
	case "all":
		scopeDesc = "the entire codebase"
	default:
		scopeDesc = scope
	}

	// Replace placeholders in the template prompt
	prompt := strings.ReplaceAll(templatePrompt, "{scope}", scopeDesc)
	prompt = strings.ReplaceAll(prompt, "{module}", targetModule)

	data := prompts.MaintenanceData{
		Prompt: prompt,
		Scope:  scopeDesc,
		Module: targetModule,
	}

	result, err := promptLoader.BuildMaintenancePrompt(data)
	if err != nil {
		// Fallback to basic prompt if template fails
		return prompt + "\n\nComplete all steps automatically."
	}
	return result
}
