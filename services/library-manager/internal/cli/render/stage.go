package render

import (
	"encoding/json"
	"io"

	"github.com/wyvernzora/kura/services/library-manager/internal/response"
)

// Stage writes the stage response. JSON-only today.
func Stage(w io.Writer, result response.StageResult, asJSON bool) error {
	_ = asJSON
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}
