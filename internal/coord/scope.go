package coord

import (
	"fmt"

	"github.com/wyvernzora/kura/internal/domain/refs"
)

// LibraryScope is the scope label used for index.tsv coordination
// errors.
const LibraryScope = "library"

// SeriesScope returns the canonical scope label for a per-series
// coordination error: "series:<ref>". Used by BusyError /
// ConflictError surfaces and by error message rendering.
func SeriesScope(ref refs.Series) string {
	return fmt.Sprintf("series:%s", ref)
}
