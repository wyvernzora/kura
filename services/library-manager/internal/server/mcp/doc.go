package mcp

import (
	"regexp"
	"strings"
)

var (
	reSchema     = regexp.MustCompile(`(?s)<!--\s*schema\s*-->.*?<!--\s*/schema\s*-->`)
	reSchemaNote = regexp.MustCompile(`(?s)<!--\s*schema-note\b.*?-->`)
)

// forLLM strips human-doc-only schema blocks before embedding markdown into
// MCP tool descriptions. Schema is already expressed in Go jsonschema tags.
func forLLM(raw string) string {
	out := reSchema.ReplaceAllString(raw, "")
	out = reSchemaNote.ReplaceAllString(out, "")
	return strings.TrimSpace(out)
}
