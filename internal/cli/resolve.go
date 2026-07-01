package cli

import (
	"errors"
)

// ErrAmbiguousSelector is returned when a CLI resolver is asked to pick
// among multiple candidates without a TTY available for the prompt.
// Surfaces translate to "specify a metadata ref" guidance.
var ErrAmbiguousSelector = errors.New("cli: selector matched multiple candidates and stdin is not a terminal")

// ErrNoMetadataMatch is returned when a CLI resolver gets back zero
// candidates from workflow.Resolve.
var ErrNoMetadataMatch = errors.New("cli: no metadata match for selector")
