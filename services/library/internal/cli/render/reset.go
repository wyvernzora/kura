package render

import (
	"encoding/json"
	"io"

	"github.com/wyvernzora/kura/services/library/internal/response"
)

// Reset writes the reset response. asJSON is the only supported mode
// today; the human shape is the same JSON because reset has no
// table-friendly columns. Future iterations can add a styled summary.
func Reset(w io.Writer, result response.ResetResult, asJSON bool) error {
	_ = asJSON
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}
