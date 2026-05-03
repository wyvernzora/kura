package render

import (
	"fmt"
	"io"

	"github.com/wyvernzora/kura/internal/cli/style"
	"github.com/wyvernzora/kura/internal/response"
)

// Remove writes the remove response. asJSON forces JSON; non-TTY also
// emits JSON. TTY emits a one-line summary.
func Remove(w io.Writer, result response.Remove, asJSON bool) error {
	if asJSON || !style.ShouldStyle(w) {
		return writeJSON(w, result)
	}
	switch result.Mode {
	case response.RemoveModePurge:
		_, err := fmt.Fprintf(w, "Purged %s (reclaimed %s).\n", result.Ref, formatBytes(result.ReclaimedBytes))
		return err
	default:
		_, err := fmt.Fprintf(w, "Untracked %s (dropped .kura/, reclaimed %s; media files left in place).\n",
			result.Ref, formatBytes(result.ReclaimedBytes))
		return err
	}
}
