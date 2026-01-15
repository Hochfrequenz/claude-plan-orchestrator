// Package prompts provides externalized prompt templates with override support.
package prompts

import "embed"

//go:embed epic/*.md maintenance/*.md skills/*.md
var embeddedFS embed.FS
