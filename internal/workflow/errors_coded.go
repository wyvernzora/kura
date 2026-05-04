package workflow

import (
	"github.com/wyvernzora/kura/internal/coord"
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

func (e *TrashAddTargetTrackedError) Kind() string     { return errkind.KindConflict }
func (e *TrashAddTargetTrackedError) Category() string { return errkind.CategoryInvalidParams }
func (e *TrashAddTargetTrackedError) Data() map[string]any {
	return map[string]any{
		"ref":        e.Ref.String(),
		"path":       e.Path,
		"episode":    e.Episode.String(),
		"recordKind": e.RecordKind,
	}
}

func (e *TrashAddTargetUnparseableError) Kind() string     { return errkind.KindInvalidEpisode }
func (e *TrashAddTargetUnparseableError) Category() string { return errkind.CategoryInvalidParams }
func (e *TrashAddTargetUnparseableError) Data() map[string]any {
	return map[string]any{
		"ref":  e.Ref.String(),
		"path": e.Path,
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

// --- reconcile lifecycle (server-side category) ------------------------

func (e *ReconcilePlanExpiredError) Kind() string     { return errkind.KindPlanExpired }
func (e *ReconcilePlanExpiredError) Category() string { return errkind.CategoryInternalError }
func (e *ReconcilePlanExpiredError) Data() map[string]any {
	return map[string]any{
		"token":     e.Token,
		"expiresAt": e.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

func (e *ReconcilePlanAlreadyAppliedError) Kind() string     { return errkind.KindPlanApplied }
func (e *ReconcilePlanAlreadyAppliedError) Category() string { return errkind.CategoryInternalError }
func (e *ReconcilePlanAlreadyAppliedError) Data() map[string]any {
	return map[string]any{"token": e.Token}
}

func (e *ReconcileInProgressError) Kind() string     { return errkind.KindBusy }
func (e *ReconcileInProgressError) Category() string { return errkind.CategoryInternalError }
func (e *ReconcileInProgressError) Data() map[string]any {
	return map[string]any{
		"token":  e.Token,
		"holder": coord.HolderData(e.Holder),
	}
}
