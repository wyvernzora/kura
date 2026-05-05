package mcp

import (
	"regexp"
	"strings"
)

var reSchema = regexp.MustCompile(`(?s)<!--\s*schema\s*-->.*?<!--\s*/schema\s*-->`)

// forLLM strips <!-- schema -->...<!-- /schema --> blocks from a markdown doc
// before embedding into MCP tool description. Schema is already expressed in
// Go jsonschema tags; the markdown blocks exist only for human-readable docs.
func forLLM(raw string) string {
	return strings.TrimSpace(reSchema.ReplaceAllString(raw, ""))
}
