package mcp

import (
	"strings"
	"testing"
)

func TestForLLMStripsHumanOnlySchemaBlocks(t *testing.T) {
	raw := `Intro.

<!-- schema-note
Human-only implementation note.
-->

Middle.

<!-- schema -->
## Parameters

- field docs
<!-- /schema -->

## Response

Outro.`

	got := forLLM(raw)
	for _, unwanted := range []string{"schema-note", "Human-only", "field docs", "<!--", "## Parameters"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("forLLM output contains %q:\n%s", unwanted, got)
		}
	}
	for _, wanted := range []string{"Intro.", "Middle.", "## Response", "Outro."} {
		if !strings.Contains(got, wanted) {
			t.Fatalf("forLLM output missing %q:\n%s", wanted, got)
		}
	}
}

func TestToolDocsDoNotExposeSchemaScaffolding(t *testing.T) {
	docs := map[string]string{
		"add":             toolAddDoc,
		"aliases":         toolAliasesDoc,
		"import":          toolImportDoc,
		"inbox_list":      toolInboxListDoc,
		"job_status":      toolJobStatusDoc,
		"list":            toolListDoc,
		"reconcile_apply": toolReconcileApplyDoc,
		"reconcile_plan":  toolReconcilePlanDoc,
		"reset":           toolResetDoc,
		"resolve":         toolResolveDoc,
		"scan":            toolScanDoc,
		"show":            toolShowDoc,
		"stage":           toolStageDoc,
	}
	for name, doc := range docs {
		t.Run(name, func(t *testing.T) {
			got := forLLM(doc)
			for _, unwanted := range []string{"<!--", "schema-note", "That Go definition is authoritative"} {
				if strings.Contains(got, unwanted) {
					t.Fatalf("forLLM(%s) exposed schema scaffolding %q:\n%s", name, unwanted, got)
				}
			}
		})
	}
}
