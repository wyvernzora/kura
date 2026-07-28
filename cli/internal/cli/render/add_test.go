package render

import (
	"bytes"
	"testing"

	"github.com/wyvernzora/kura/services/library-manager/pkg/api"
	"github.com/wyvernzora/kura/services/library-manager/pkg/api/refs"
)

// AddResult carries a metadata ref and a directory that are both
// string-ish, so printing the wrong one compiles and vets clean. This
// pins which one the human-readable line is supposed to carry.
func TestAdd_PrintsDirectoryNotRef(t *testing.T) {
	dir, err := refs.ParseSeries("Bookworm")
	if err != nil {
		t.Fatalf("ParseSeries: %v", err)
	}
	var out bytes.Buffer
	result := api.AddResult{
		Ref:            refs.Metadata("tvdb:370070"),
		Directory:      dir,
		PreferredTitle: "Ascendance of a Bookworm",
	}
	if err := Add(&out, result, "", false); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got := out.String(); got != "Added Bookworm\n" {
		t.Errorf("output = %q, want %q", got, "Added Bookworm\n")
	}
}
