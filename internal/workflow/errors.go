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

// MetadataRefNotIndexedError signals the requested metadata ref has no
// matching series under the library root.
type MetadataRefNotIndexedError struct {
	Ref refs.Metadata
}

func (e *MetadataRefNotIndexedError) Error() string {
	return fmt.Sprintf("workflow: metadata ref %s is not indexed", e.Ref)
}

// MetadataMissingEpisodeError signals the requested episode does not
// exist in the series's metadata-derived spine. The user typed a slot
// that the provider doesn't know about.
type MetadataMissingEpisodeError struct {
	Episode refs.Episode
}

func (e *MetadataMissingEpisodeError) Error() string {
	return fmt.Sprintf("workflow: metadata has no %s", e.Episode.Marker())
}

// NoStagedEpisodeError signals the requested episode has no staged
// record to drop (reset is a no-op).
type NoStagedEpisodeError struct {
	Episode refs.Episode
}

func (e *NoStagedEpisodeError) Error() string {
	return fmt.Sprintf("workflow: episode %s has no staged media", e.Episode.Marker())
}

// EpisodeAlreadyExistsError signals an active record is present for the
// episode and the operation requires --replace to overwrite it.
type EpisodeAlreadyExistsError struct {
	Episode refs.Episode
}

func (e *EpisodeAlreadyExistsError) Error() string {
	return fmt.Sprintf("workflow: episode %s already has active media; pass replace to replace it", e.Episode.Marker())
}

// StagedEpisodeAlreadyExistsError signals a staged record is present
// for the episode and the operation requires --replace to overwrite it.
type StagedEpisodeAlreadyExistsError struct {
	Episode refs.Episode
}

func (e *StagedEpisodeAlreadyExistsError) Error() string {
	return fmt.Sprintf("workflow: staged episode %s already exists; pass replace to replace it", e.Episode.Marker())
}
