package workflow

import "errors"

// ErrLibraryRootNotFound is returned when the configured library root
// directory does not exist on disk.
var ErrLibraryRootNotFound = errors.New("workflow: library root not found")

// ErrLibraryRootNotDirectory is returned when the library root path
// exists but is not a directory.
var ErrLibraryRootNotDirectory = errors.New("workflow: library root is not a directory")
