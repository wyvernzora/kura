package render

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/wyvernzora/kura/internal/response"
)

// Add writes the add response. asJSON toggles JSON; otherwise prints
// "Added <ref> (<metadataRef>)\n".
func Add(w io.Writer, result response.AddResult, verb string, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	if verb == "" {
		verb = "Added"
	}
	_, err := fmt.Fprintf(w, "%s %s (%s)\n", verb, result.Ref, result.MetadataRef)
	return err
}
