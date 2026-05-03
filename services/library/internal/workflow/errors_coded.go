package workflow

import (
	"github.com/wyvernzora/kura/internal/errkind"
)

// This file attaches the Kind / Category / Data methods used by
// surface error mapping (errkind.Coded) to the typed errors declared
// in errors.go. Splitting the methods out keeps the error declarations
// readable and lets new typed errors document their taxonomy in one
// place.

// --- not-found family --------------------------------------------------

func (e *NotFoundError) Kind() string     { return errkind.KindNotFound }
func (e *NotFoundError) Category() string { return errkind.CategoryInvalidParams }
func (e *NotFoundError) Data() map[string]any {
	return map[string]any{"ref": e.Ref.String()}
}

func (e *MetadataRefNotIndexedError) Kind() string     { return errkind.KindNotFound }
func (e *MetadataRefNotIndexedError) Category() string { return errkind.CategoryInvalidParams }
func (e *MetadataRefNotIndexedError) Data() map[string]any {
	return map[string]any{"ref": e.Ref.String()}
}

func (e *SeriesNotFoundError) Kind() string     { return errkind.KindNotFound }
func (e *SeriesNotFoundError) Category() string { return errkind.CategoryInvalidParams }
func (e *SeriesNotFoundError) Data() map[string]any {
	return map[string]any{"ref": e.Ref.String()}
}

func (e *EpisodeSelectorSeasonMissingError) Kind() string     { return errkind.KindNotFound }
func (e *EpisodeSelectorSeasonMissingError) Category() string { return errkind.CategoryInvalidParams }
func (e *EpisodeSelectorSeasonMissingError) Data() map[string]any {
	return map[string]any{
		"ref":      e.Ref.String(),
		"selector": e.Selector,
		"season":   e.Season,
	}
}

// --- conflict family ---------------------------------------------------

func (e *MetadataRefConflictError) Kind() string     { return errkind.KindConflict }
func (e *MetadataRefConflictError) Category() string { return errkind.CategoryInvalidParams }
func (e *MetadataRefConflictError) Data() map[string]any {
	return map[string]any{
		"ref":      e.Ref.String(),
		"existing": e.Existing.String(),
		"next":     e.Next.String(),
	}
}

func (e *SeriesAlreadyExistsError) Kind() string     { return errkind.KindConflict }
func (e *SeriesAlreadyExistsError) Category() string { return errkind.CategoryInvalidParams }
func (e *SeriesAlreadyExistsError) Data() map[string]any {
	return map[string]any{"ref": e.Ref.String()}
}

func (e *SeriesAlreadyTrackedError) Kind() string     { return errkind.KindConflict }
func (e *SeriesAlreadyTrackedError) Category() string { return errkind.CategoryInvalidParams }
func (e *SeriesAlreadyTrackedError) Data() map[string]any {
	return map[string]any{"ref": e.Ref.String()}
}

func (e *EpisodeAlreadyExistsError) Kind() string     { return errkind.KindConflict }
func (e *EpisodeAlreadyExistsError) Category() string { return errkind.CategoryInvalidParams }
func (e *EpisodeAlreadyExistsError) Data() map[string]any {
	return map[string]any{"episode": e.Episode.String()}
}

func (e *StagedEpisodeAlreadyExistsError) Kind() string     { return errkind.KindConflict }
func (e *StagedEpisodeAlreadyExistsError) Category() string { return errkind.CategoryInvalidParams }
func (e *StagedEpisodeAlreadyExistsError) Data() map[string]any {
	return map[string]any{"episode": e.Episode.String()}
}

func (e *TrashRestoreTargetExistsError) Kind() string     { return errkind.KindConflict }
func (e *TrashRestoreTargetExistsError) Category() string { return errkind.CategoryInvalidParams }
func (e *TrashRestoreTargetExistsError) Data() map[string]any {
	return map[string]any{
		"ref":     e.Ref.String(),
		"id":      e.ID,
		"targets": e.Targets,
	}
}

func (e *RemoveStagedRecordsExistError) Kind() string     { return errkind.KindConflict }
func (e *RemoveStagedRecordsExistError) Category() string { return errkind.CategoryInvalidParams }
func (e *RemoveStagedRecordsExistError) Data() map[string]any {
	episodes := make([]string, len(e.Episodes))
	for i, ep := range e.Episodes {
		episodes[i] = ep.String()
	}
	return map[string]any{
		"ref":      e.Ref.String(),
		"episodes": episodes,
	}
}

// --- episode-level invalid params --------------------------------------

func (e *MetadataMissingEpisodeError) Kind() string     { return errkind.KindInvalidEpisode }
func (e *MetadataMissingEpisodeError) Category() string { return errkind.CategoryInvalidParams }
func (e *MetadataMissingEpisodeError) Data() map[string]any {
	return map[string]any{"episode": e.Episode.String()}
}

func (e *NoStagedEpisodeError) Kind() string     { return errkind.KindNoStaged }
func (e *NoStagedEpisodeError) Category() string { return errkind.CategoryInvalidParams }
func (e *NoStagedEpisodeError) Data() map[string]any {
	return map[string]any{"episode": e.Episode.String()}
}

// --- provider misconfiguration -----------------------------------------

func (e *UnsupportedMetadataSourceError) Kind() string     { return errkind.KindUnsupportedProvider }
func (e *UnsupportedMetadataSourceError) Category() string { return errkind.CategoryInvalidParams }
func (e *UnsupportedMetadataSourceError) Data() map[string]any {
	return map[string]any{"source": e.Source}
}

// reconcile lifecycle errors (PlanExpiredError, PlanAlreadyAppliedError,
// InProgressError, StaleSnapshotError) live in internal/reconcile and
// implement Coded directly there.

// --- list pagination + lifecycle ---------------------------------------

func (e *InvalidCursorError) Kind() string     { return errkind.KindInvalidCursor }
func (e *InvalidCursorError) Category() string { return errkind.CategoryInvalidParams }
func (e *InvalidCursorError) Data() map[string]any {
	if e.Reason == "" {
		return nil
	}
	return map[string]any{"reason": e.Reason}
}

func (e *ServerNotReadyError) Kind() string     { return errkind.KindServerNotReady }
func (e *ServerNotReadyError) Category() string { return errkind.CategoryInternalError }
func (e *ServerNotReadyError) Data() map[string]any {
	if e.Reason == "" {
		return nil
	}
	return map[string]any{"reason": e.Reason}
}

// --- inbox listing -----------------------------------------------------

func (e *InboxLimitTooLargeError) Kind() string     { return errkind.KindInvalidRef }
func (e *InboxLimitTooLargeError) Category() string { return errkind.CategoryInvalidParams }
func (e *InboxLimitTooLargeError) Data() map[string]any {
	return map[string]any{"limit": e.Limit, "max": e.Max}
}

func (e *InboxDepthTooLargeError) Kind() string     { return errkind.KindInvalidRef }
func (e *InboxDepthTooLargeError) Category() string { return errkind.CategoryInvalidParams }
func (e *InboxDepthTooLargeError) Data() map[string]any {
	return map[string]any{"depth": e.Depth, "max": e.Max}
}

func (e *InboxNotConfiguredError) Kind() string     { return errkind.KindServerNotReady }
func (e *InboxNotConfiguredError) Category() string { return errkind.CategoryInternalError }
func (e *InboxNotConfiguredError) Data() map[string]any {
	return nil
}
