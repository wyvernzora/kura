package workflow

import (
	"errors"
	"fmt"
	"time"

	"github.com/wyvernzora/kura/internal/coord"
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

// ReconcilePlanExpiredError signals the plan TTL has elapsed before
// apply was called. Caller re-plans.
type ReconcilePlanExpiredError struct {
	Token     string
	ExpiresAt time.Time
}

func (e *ReconcilePlanExpiredError) Error() string {
	return fmt.Sprintf("workflow: reconcile plan %s expired at %s", e.Token, e.ExpiresAt.Format(time.RFC3339))
}

// ReconcilePlanAlreadyAppliedError signals the plan log already has a
// success record. Apply refuses to re-apply.
type ReconcilePlanAlreadyAppliedError struct {
	Token string
}

func (e *ReconcilePlanAlreadyAppliedError) Error() string {
	return fmt.Sprintf("workflow: reconcile plan %s was already applied", e.Token)
}

// ReconcileInProgressError signals an apply is already running for the
// same plan token (same caller retry, or duplicate request). The Holder
// identifies the original apply.
type ReconcileInProgressError struct {
	Token  string
	Holder coord.Holder
}

func (e *ReconcileInProgressError) Error() string {
	return fmt.Sprintf("workflow: reconcile apply for token %s is already in progress on host=%s pid=%d since %s",
		e.Token, e.Holder.Host, e.Holder.PID, e.Holder.Started.Format(time.RFC3339))
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

// TrashAddTargetTrackedError signals the requested file is currently
// referenced as the active or staged record for some episode in the
// series. TrashAdd refuses to displace tracked records implicitly;
// caller must use stage --replace + reconcile (active) or reset
// (staged) to clear the record first.
type TrashAddTargetTrackedError struct {
	Ref        refs.Series
	Path       string
	Episode    refs.Episode
	RecordKind string // "active" or "staged"
}

func (e *TrashAddTargetTrackedError) Error() string {
	return fmt.Sprintf("workflow: trash add %q: file is the %s record for %s on %s; use stage/reset+reconcile to clear it first",
		e.Path, e.RecordKind, e.Episode.Marker(), e.Ref)
}

// TrashAddTargetUnparseableError signals the requested file is not in
// a recognized series-tree location or its filename cannot be parsed
// to a (season, episode) slot. TrashAdd needs both to record a
// trash-restorable entry; orphan files require manual fs cleanup.
type TrashAddTargetUnparseableError struct {
	Ref  refs.Series
	Path string
}

func (e *TrashAddTargetUnparseableError) Error() string {
	return fmt.Sprintf("workflow: trash add %q: filename does not parse to a season/episode; manual cleanup required", e.Path)
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
