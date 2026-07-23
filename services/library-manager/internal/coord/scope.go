package coord

import (
	"fmt"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
)

// SeriesScope returns the canonical scope label for a per-series coordination
// error: "series:<ref>".
func SeriesScope(ref refs.Series) string {
	return fmt.Sprintf("series:%s", ref)
}
