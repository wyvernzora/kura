package render

import (
	"fmt"
	"io"

	"github.com/wyvernzora/kura/services/library-manager/internal/cli/style"
	"github.com/wyvernzora/kura/services/library-manager/internal/response"
)

// Remove writes the remove response. asJSON forces JSON; non-TTY also
// emits JSON. TTY emits a one-line summary. The verb (Purged vs
// Untracked) and series ref are passed by the caller since the
// response no longer echoes them.
func Remove(w io.Writer, result response.Remove, ref string, purged bool, asJSON bool) error {
	if asJSON || !style.ShouldStyle(w) {
		return writeJSON(w, result)
	}
	if purged {
		_, err := fmt.Fprintf(w, "Purged %s (reclaimed %s).\n", ref, formatBytes(result.ReclaimedBytes))
		return err
	}
	_, err := fmt.Fprintf(w, "Untracked %s (dropped .kura/, reclaimed %s; media files left in place).\n",
		ref, formatBytes(result.ReclaimedBytes))
	return err
}
