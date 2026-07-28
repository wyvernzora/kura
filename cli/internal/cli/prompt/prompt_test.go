package prompt

import (
	"bytes"
	"testing"
)

func TestWriteHintUsesPublicRefVocabulary(t *testing.T) {
	var out bytes.Buffer

	WriteHint(&out, nil)

	const want = "retry with one of: kura <cmd> <ref>\n"
	if got := out.String(); got != want {
		t.Fatalf("WriteHint() = %q, want %q", got, want)
	}
}
