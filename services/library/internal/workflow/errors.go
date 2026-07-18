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

// InvalidTagError reports invalid tag syntax or contradictory expressions.
type InvalidTagError struct {
	Tag    string
	Reason string
	Limit  int
}

func (e *InvalidTagError) Error() string {
	if e.Tag != "" {
		return fmt.Sprintf("workflow: invalid tag %q: %s", e.Tag, e.Reason)
	}
	return "workflow: invalid tags: " + e.Reason
}

// NotFoundError signals a series is not present in the library. Surfaces
// translate to a 404-style response (CLI exit code, MCP error code).
type NotFoundError struct {
	Ref refs.Series
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("workflow: series %q not found", e.Ref)
}

// ReconcilePlanNotFoundError signals a reconcile apply token does not
// identify a saved plan for the requested series.
type ReconcilePlanNotFoundError struct {
	Ref   refs.Series
	Token string
}

func (e *ReconcilePlanNotFoundError) Error() string {
	return fmt.Sprintf("workflow: reconcile plan %s for series %q not found", e.Token, e.Ref)
}

// MetadataRefNotIndexedError signals the requested metadata ref has no
// matching series under the library root.
type MetadataRefNotIndexedError struct {
	Ref refs.Metadata
}

func (e *MetadataRefNotIndexedError) Error() string {
	return fmt.Sprintf("workflow: metadata ref %s is not indexed", e.Ref)
}

// EpisodeSelectorSeasonMissingError signals the season requested by an
// episodes selector (e.g. "S07") is not present in the series's spine.
// Caller should retry with a valid season; the response surface
// translates to a "not found"-style error.
type EpisodeSelectorSeasonMissingError struct {
	Ref      refs.Series
	Selector string
	Season   int
}

func (e *EpisodeSelectorSeasonMissingError) Error() string {
	return fmt.Sprintf("workflow: series %q has no season %d (selector %q)", e.Ref, e.Season, e.Selector)
}

// MetadataRefConflictError signals the metadata ref is already tracked
// at a different series ref. Add/import refuse.
type MetadataRefConflictError struct {
	Ref      refs.Metadata
	Existing refs.Series
	Next     refs.Series
}

func (e *MetadataRefConflictError) Error() string {
	return fmt.Sprintf("workflow: metadata ref %s is already tracked at %q", e.Ref, e.Existing)
}

// SeriesAlreadyExistsError signals the target series directory exists.
// Add refuses; the operator picks a different ref or removes the dir.
type SeriesAlreadyExistsError struct {
	Ref refs.Series
}

func (e *SeriesAlreadyExistsError) Error() string {
	return fmt.Sprintf("workflow: series %q already exists below the library root", e.Ref)
}

// SeriesNotFoundError signals an import target directory does not exist
// under the library root.
type SeriesNotFoundError struct {
	Ref refs.Series
}

func (e *SeriesNotFoundError) Error() string {
	return fmt.Sprintf("workflow: series %q does not exist below the library root", e.Ref)
}

// SeriesAlreadyTrackedError signals the target directory already has
// .kura/series.json. Import refuses without --force.
type SeriesAlreadyTrackedError struct {
	Ref refs.Series
}

func (e *SeriesAlreadyTrackedError) Error() string {
	return fmt.Sprintf("workflow: series %q already has .kura/series.json", e.Ref)
}

// UnsupportedMetadataSourceError signals the resolved metadata ref's
// provider does not match the configured source.
type UnsupportedMetadataSourceError struct {
	Source string
}

func (e *UnsupportedMetadataSourceError) Error() string {
	return fmt.Sprintf("workflow: unsupported metadata ref source %q", e.Source)
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

// TrashRestoreTargetExistsError signals one or more recorded paths in
// the trash entry already exist on disk. Restore refuses to overwrite;
// the operator must move the conflicting files out of the way first.
type TrashRestoreTargetExistsError struct {
	Ref     refs.Series
	ID      string
	Targets []string
}

func (e *TrashRestoreTargetExistsError) Error() string {
	if len(e.Targets) == 1 {
		return fmt.Sprintf("workflow: trash restore %s for %q blocked: %s already exists", e.ID, e.Ref, e.Targets[0])
	}
	return fmt.Sprintf("workflow: trash restore %s for %q blocked: %d target paths already exist", e.ID, e.Ref, len(e.Targets))
}

// InvalidCursorError signals the pagination cursor passed to List is
// malformed (wrong length, bad encoding, or anchor missing despite a
// matching view hash). Surfaces as KindInvalidCursor.
type InvalidCursorError struct {
	Reason string
}

func (e *InvalidCursorError) Error() string {
	return fmt.Sprintf("workflow: invalid cursor: %s", e.Reason)
}

// ServerNotReadyError signals the in-memory index is rebuilding from a
// cold start (or corruption recovery) and has nothing to serve. Clients
// should retry shortly. Surfaces as KindServerNotReady.
type ServerNotReadyError struct {
	Reason string
}

func (e *ServerNotReadyError) Error() string {
	if e.Reason == "" {
		return "workflow: index is rebuilding; retry shortly"
	}
	return fmt.Sprintf("workflow: index is rebuilding (%s); retry shortly", e.Reason)
}

// RemoveStagedRecordsExistError signals the series has staged records
// blocking a default (non-purge) remove. Caller must reconcile or reset
// --all first; --purge bypasses the gate.
type RemoveStagedRecordsExistError struct {
	Ref      refs.Series
	Episodes []refs.Episode
}

func (e *RemoveStagedRecordsExistError) Error() string {
	if len(e.Episodes) == 1 {
		return fmt.Sprintf("workflow: remove %q blocked: staged record %s exists; reset/reconcile first or use --purge", e.Ref, e.Episodes[0].Marker())
	}
	return fmt.Sprintf("workflow: remove %q blocked: %d staged records exist; reset/reconcile first or use --purge", e.Ref, len(e.Episodes))
}

// EmptyStageBatchError signals stage was called with no items in any
// of the three input arrays (episodes, trash, extras). Phase 1 reject.
type EmptyStageBatchError struct{}

func (e *EmptyStageBatchError) Error() string {
	return "workflow: stage batch is empty (need at least one item across episodes, trash, extras)"
}

// DuplicateBatchEpisodeError signals two episode items in the same
// stage batch target the same (season, episode) slot. Phase 1 reject.
type DuplicateBatchEpisodeError struct {
	Episode refs.Episode
}

func (e *DuplicateBatchEpisodeError) Error() string {
	return fmt.Sprintf("workflow: duplicate episode %s in stage batch", e.Episode.Marker())
}

// DuplicateBatchPathError signals the same file path appears twice in a
// stage batch (e.g. once in trash[] and once as a companion). Phase 1.
type DuplicateBatchPathError struct {
	Path string
}

func (e *DuplicateBatchPathError) Error() string {
	return fmt.Sprintf("workflow: path %q referenced more than once in stage batch", e.Path)
}

// TrashTargetTrackedError signals a trash item targets a file that is
// the active or staged record (or companion of one) for an episode.
// Caller must use stage --replace + reconcile or reset to release the
// record before trashing.
type TrashTargetTrackedError struct {
	Path       string
	Episode    refs.Episode
	RecordKind string // "active" or "staged"
}

func (e *TrashTargetTrackedError) Error() string {
	return fmt.Sprintf("workflow: trash target %q is the %s record for %s; release the record first", e.Path, e.RecordKind, e.Episode.Marker())
}

// TrashAlreadyStagedError signals a trash item is already in
// stagedTrash[] (queued by an earlier batch). Caller can reset --trash
// <ulid> if they want to re-queue.
type TrashAlreadyStagedError struct {
	Path string
}

func (e *TrashAlreadyStagedError) Error() string {
	return fmt.Sprintf("workflow: trash target %q is already queued for trash on next reconcile", e.Path)
}

// ExtraSeasonMissingError signals an extras item references a season
// the series spine does not have.
type ExtraSeasonMissingError struct {
	Season int
}

func (e *ExtraSeasonMissingError) Error() string {
	return fmt.Sprintf("workflow: extras: season %d is not present in the series spine", e.Season)
}

// ExtraPrefixInvalidError signals the extras prefix failed sanitization.
type ExtraPrefixInvalidError struct {
	Prefix string
	Reason string
}

func (e *ExtraPrefixInvalidError) Error() string {
	return fmt.Sprintf("workflow: extras prefix %q invalid: %s", e.Prefix, e.Reason)
}

// ExtraTargetCollisionError signals an extras item's destination
// (Season N/Extra/[prefix]/<basename>) already exists on disk or is
// targeted by another item earlier in the same batch.
type ExtraTargetCollisionError struct {
	Path string
}

func (e *ExtraTargetCollisionError) Error() string {
	return fmt.Sprintf("workflow: extras target %q already exists or is targeted twice in the same batch", e.Path)
}
