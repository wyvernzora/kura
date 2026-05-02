package workflow

import (
	"errors"
	"fmt"

	"github.com/wyvernzora/kura/internal/domain/refs"
)

// ErrLibraryRootNotFound is returned when the configured library root
// directory does not exist on disk.
var ErrLibraryRootNotFound = errors.New("workflow: library root not found")

// ErrLibraryRootNotDirectory is returned when the library root path
// exists but is not a directory.
var ErrLibraryRootNotDirectory = errors.New("workflow: library root is not a directory")

// NotFoundError signals a series is not present in the library. Surfaces
// translate to a 404-style response (CLI exit code, MCP error code).
type NotFoundError struct {
	Ref refs.Series
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("workflow: series %q not found", e.Ref)
}
