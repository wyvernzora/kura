package render

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/wyvernzora/kura/services/library-manager/pkg/api"
)

// Add writes the add response. asJSON toggles JSON; otherwise prints
// "Added <directory>\n". The directory is the informative half: the
// caller already supplied or resolved the metadata ref, whereas the
// sanitized on-disk basename is not trivially derivable from an
// arbitrary title.
func Add(w io.Writer, result api.AddResult, verb string, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	if verb == "" {
		verb = "Added"
	}
	_, err := fmt.Fprintf(w, "%s %s\n", verb, result.Directory)
	return err
}
