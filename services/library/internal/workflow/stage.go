package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/media"
	domainrefs "github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/domain/selector"
	domainseries "github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/jobs"
	"github.com/wyvernzora/kura/internal/mediainfo"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
)

// StageInput parameters for the Stage workflow. Stage queues per-batch
// changes to series.json; the file moves themselves happen on reconcile_
// apply. A batch can mix any combination of episode stages, trash items,
// and extras placements; at least one item across the three slices is
// required.
//
// Each input array follows phase-split validation: phase 1 (whole-batch
// reject) catches input shape and cross-state conflicts before any file
// is touched; phase 2 (best-effort per-item) probes mediainfo for episode
// items and stats trash/extras paths, with per-item failures landing in
// response.StageResult.Skipped[].
type StageInput struct {
	Ref      domainrefs.Series
	Episodes []EpisodeStageItem
	Trash    []TrashStageItem
	Extras   []ExtraStageItem
}

// EpisodeStageItem queues one episode stage. Media is an inbox:
// selector identifying the source file under the inbox root; the
// workflow resolves it via deps.InboxRoot at validation time.
// Companions are sibling files (subs, NFOs, etc.) — same selector
// shape.
type EpisodeStageItem struct {
	Episode    domainrefs.Episode
	Media      selector.Path
	Source     string
	Companions []selector.Path
	Replace    bool
}

// TrashStageItem queues one file (and explicit companions) for trash on
// the next reconcile_apply. Path is a series: selector scoped to the
// request's series ref (resolved via paths.SeriesDir at validation
// time). Trash items live entirely inside the library, so the series:
// scheme captures their semantics.
type TrashStageItem struct {
	Path       selector.Path
	Companions []selector.Path
}

// ExtraStageItem queues one extras placement. Source is an inbox:
// selector pointing at a file or directory under the inbox root.
// Prefix is an optional sub-folder under Season N/Extra/.
type ExtraStageItem struct {
	Season int
	Source selector.Path
	Prefix string
}

// Stage queues per-batch staging changes to series.json. Returns a
// tracked *jobs.Job; CLI callers Wait for the typed result, MCP callers
// hand the ID off to a polling client.
//
// Wrapped in coord.WithSeries with caller-side coord.RetryOnConflict:
// a peer CAS-conflict triggers retries up to KURA_CONFLICT_RETRIES+1
// before surfacing.
func Stage(ctx context.Context, deps Deps, in StageInput) *jobs.Job[response.StageResult] {
	return jobs.Submit(deps.Jobs, ctx, jobs.KindStage, in.Ref, func(jobCtx context.Context) (response.StageResult, error) {
		var out response.StageResult
		err := deps.Coordinator.WithSeries(jobCtx, in.Ref, func() error {
			return coord.RetryOnConflict(coord.AttemptsFromEnv(), func() error {
				result, runErr := stageBatch(jobCtx, deps, in)
				if runErr != nil {
					return runErr
				}
				out = result
				return nil
			})
		})
		return out, err
	})
}

func stageBatch(ctx context.Context, deps Deps, in StageInput) (response.StageResult, error) {
	totalItems := len(in.Episodes) + len(in.Trash) + len(in.Extras)
	progress.Start(ctx, "stage", fmt.Sprintf("Staging %d item(s) for %s", totalItems, in.Ref), 0)
	// Single deferred failure path so new error returns don't have to
	// remember a per-site progress.Failure call. succeeded is flipped
	// to true only on the success-return path below.
	succeeded := false
	defer func() {
		if !succeeded {
			progress.Failure(ctx, "stage", fmt.Sprintf("Failed to stage for %s", in.Ref), 0, totalItems)
		}
	}()

	if totalItems == 0 {
		return response.StageResult{}, &EmptyStageBatchError{}
	}

	seriesRoot := paths.SeriesDir(deps.LibRoot, in.Ref)
	model, err := seriesfile.Load(deps.LibRoot, in.Ref)
	if err != nil {
		return response.StageResult{}, err
	}
	if model.InProgress != nil {
		return response.StageResult{}, &coord.BusyError{Scope: coord.SeriesScope(in.Ref), Holder: *model.InProgress}
	}

	// Phase 1: validation. Resolve paths once so phase 2 doesn't re-pay
	// the resolution cost; carries forward into per-item work.
	episodes, err := validateEpisodeItems(deps, model, in.Episodes)
	if err != nil {
		return response.StageResult{}, err
	}
	trash, err := validateTrashItems(seriesRoot, model, in.Trash)
	if err != nil {
		return response.StageResult{}, err
	}
	extras, err := validateExtraItems(deps, seriesRoot, model, in.Extras)
	if err != nil {
		return response.StageResult{}, err
	}

	// Phase 2: per-item work. Apply successes to model; collect skipped[].
	out := response.StageResult{}
	now := deps.Now().UTC()

	for index, item := range episodes {
		progress.Update(ctx, "stage", fmt.Sprintf("Inspecting %s", filepath.Base(item.resolvedPath)), index+1, totalItems)
		result, skip, err := applyEpisodeItem(ctx, deps, seriesRoot, model, item)
		if err != nil {
			return response.StageResult{}, err
		}
		if skip != nil {
			out.Skipped = append(out.Skipped, *skip)
			continue
		}
		out.Episodes = append(out.Episodes, result)
	}

	for index, item := range trash {
		progress.Update(ctx, "stage", fmt.Sprintf("Queuing trash %s", filepath.Base(item.resolvedPath)), len(episodes)+index+1, totalItems)
		result, skip, err := applyTrashItem(model, item, now)
		if err != nil {
			return response.StageResult{}, err
		}
		if skip != nil {
			out.Skipped = append(out.Skipped, *skip)
			continue
		}
		out.Trash = append(out.Trash, result)
	}

	for index, item := range extras {
		progress.Update(ctx, "stage", fmt.Sprintf("Queuing extra %s", filepath.Base(item.resolvedPath)), len(episodes)+len(trash)+index+1, totalItems)
		result, skip, err := applyExtraItem(model, item, now)
		if err != nil {
			return response.StageResult{}, err
		}
		if skip != nil {
			out.Skipped = append(out.Skipped, *skip)
			continue
		}
		out.Extras = append(out.Extras, result)
	}

	if err := seriesfile.SaveCAS(deps.LibRoot, model, coord.NewMutator("stage")); err != nil {
		return response.StageResult{}, err
	}
	if err := updateIndexRow(ctx, deps, model, "stage"); err != nil {
		return response.StageResult{}, err
	}

	progress.Success(ctx, "stage",
		fmt.Sprintf("Staged %d ep + %d trash + %d extras (%d skipped) for %s",
			len(out.Episodes), len(out.Trash), len(out.Extras), len(out.Skipped), in.Ref),
		totalItems)
	succeeded = true
	return out, nil
}

// resolvedEpisodeItem carries the original input plus the resolved
// absolute paths (post selector lookup + EvalSymlinks + prefix check)
// after phase 1 validation. resolvedSelector and resolvedCompanionSels
// preserve the inbox: form for persistence in the staged record;
// mediainfo and stat operations use the resolved abs path.
type resolvedEpisodeItem struct {
	EpisodeStageItem
	resolvedPath          string
	resolvedCompanions    []string
	resolvedCompanionSels []selector.Path
	episodeStateBefore    domainseries.Episode
}

type resolvedTrashItem struct {
	TrashStageItem
	id                 ulid.ULID
	resolvedPath       string
	resolvedCompanions []string
	relPath            string
	relCompanions      []string
}

type resolvedExtraItem struct {
	ExtraStageItem
	id           ulid.ULID
	resolvedPath string
	isDir        bool
	relTarget    string // wire-shape destination under Season N/Extra/[prefix]/<basename>
}

// resolveInboxSelector validates a selector, resolves it to an absolute
// path under inboxRoot, and confirms the result stays within the inbox
// (catches symlink escapes). Returns the resolved abs path on success.
func resolveInboxSelector(inboxRoot string, sel selector.Path) (string, error) {
	if inboxRoot == "" {
		return "", &InboxNotConfiguredError{}
	}
	if sel.IsZero() {
		return "", errors.New("inbox selector is empty")
	}
	abs := sel.Resolve(inboxRoot)
	rootAbs, err := filepath.Abs(inboxRoot)
	if err != nil {
		return "", err
	}
	rootResolved, rootErr := filepath.EvalSymlinks(rootAbs)
	target, targetErr := filepath.EvalSymlinks(abs)
	if rootErr != nil || targetErr != nil {
		rootResolved = filepath.Clean(rootAbs)
		target = filepath.Clean(abs)
	}
	rel, err := filepath.Rel(rootResolved, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", &selector.PathOutsideRootError{Path: sel.String()}
	}
	return abs, nil
}

func validateEpisodeItems(deps Deps, model *domainseries.Series, items []EpisodeStageItem) ([]resolvedEpisodeItem, error) {
	out := make([]resolvedEpisodeItem, 0, len(items))
	seenEpisodes := map[domainrefs.Episode]struct{}{}
	for index, item := range items {
		if _, dup := seenEpisodes[item.Episode]; dup {
			return nil, &DuplicateBatchEpisodeError{Episode: item.Episode}
		}
		seenEpisodes[item.Episode] = struct{}{}

		mediaPath, err := resolveInboxSelector(deps.InboxRoot, item.Media)
		if err != nil {
			return nil, fmt.Errorf("episodes[%d]: %w", index, err)
		}
		info, err := os.Lstat(mediaPath)
		if err != nil {
			return nil, fmt.Errorf("episodes[%d]: %w", index, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("episodes[%d]: %q is a directory", index, item.Media)
		}
		if !mediainfo.RecognizedVideoFile(mediaPath) {
			return nil, fmt.Errorf("episodes[%d]: %q is not a recognized video file", index, item.Media)
		}
		episodeState, ok := model.Episodes[item.Episode]
		if !ok {
			return nil, &MetadataMissingEpisodeError{Episode: item.Episode}
		}
		if episodeState.Active != nil && !item.Replace {
			if _, statErr := os.Stat(episodeState.Active.Path); statErr == nil {
				return nil, &EpisodeAlreadyExistsError{Episode: item.Episode}
			}
		}
		if episodeState.Staged != nil && !item.Replace {
			return nil, &StagedEpisodeAlreadyExistsError{Episode: item.Episode}
		}
		companions := make([]string, 0, len(item.Companions))
		companionSels := make([]selector.Path, 0, len(item.Companions))
		for _, sel := range item.Companions {
			c, err := resolveInboxSelector(deps.InboxRoot, sel)
			if err != nil {
				return nil, fmt.Errorf("episodes[%d] companion: %w", index, err)
			}
			cInfo, statErr := os.Lstat(c)
			if statErr != nil {
				return nil, fmt.Errorf("episodes[%d] companion: %w", index, statErr)
			}
			if cInfo.IsDir() {
				return nil, fmt.Errorf("episodes[%d] companion %q is a directory", index, sel)
			}
			companions = append(companions, c)
			companionSels = append(companionSels, sel)
		}
		out = append(out, resolvedEpisodeItem{
			EpisodeStageItem:      item,
			resolvedPath:          mediaPath,
			resolvedCompanions:    companions,
			resolvedCompanionSels: companionSels,
			episodeStateBefore:    episodeState,
		})
	}
	return out, nil
}

// validateTrashItems enforces the four trash invariants in addition to
// selector resolution and per-batch dedup. Trash inputs are series:
// selectors scoped to seriesRoot (the request's series ref).
func validateTrashItems(seriesRoot string, model *domainseries.Series, items []TrashStageItem) ([]resolvedTrashItem, error) {
	out := make([]resolvedTrashItem, 0, len(items))
	seenPaths := map[string]struct{}{}

	// Build lookup sets from current series state.
	activePaths := map[string]domainrefs.Episode{}
	stagedPaths := map[string]domainrefs.Episode{}
	for ref, ep := range model.Episodes {
		if ep.Active != nil {
			activePaths[ep.Active.Path] = ref
			for _, c := range ep.Active.Companions {
				activePaths[c.Path] = ref
			}
		}
		if ep.Staged != nil {
			stagedPaths[ep.Staged.Path] = ref
			for _, c := range ep.Staged.Companions {
				stagedPaths[c.Path] = ref
			}
		}
	}
	stagedTrashPaths := map[string]struct{}{}
	for _, t := range model.StagedTrash {
		stagedTrashPaths[t.Path] = struct{}{}
		for _, c := range t.Companions {
			stagedTrashPaths[c.Path] = struct{}{}
		}
	}

	for index, item := range items {
		if item.Path.IsZero() {
			return nil, fmt.Errorf("trash[%d]: path selector is empty", index)
		}
		if item.Path.Scheme != selector.Series {
			return nil, fmt.Errorf("trash[%d]: expected series: selector, got %q", index, item.Path.Scheme)
		}
		absPath := item.Path.Resolve(seriesRoot)
		relPath := item.Path.Relative
		// Invariant: not duplicated within batch.
		if _, dup := seenPaths[absPath]; dup {
			return nil, &DuplicateBatchPathError{Path: absPath}
		}
		seenPaths[absPath] = struct{}{}
		// Invariant: not active or companion of active.
		if ep, isActive := activePaths[absPath]; isActive {
			return nil, &TrashTargetTrackedError{Path: relPath, Episode: ep, RecordKind: "active"}
		}
		// Invariant: not staged or companion of staged.
		if ep, isStaged := stagedPaths[absPath]; isStaged {
			return nil, &TrashTargetTrackedError{Path: relPath, Episode: ep, RecordKind: "staged"}
		}
		// Invariant: not already in stagedTrash[].
		if _, dup := stagedTrashPaths[absPath]; dup {
			return nil, &TrashAlreadyStagedError{Path: relPath}
		}

		companions := make([]string, 0, len(item.Companions))
		relCompanions := make([]string, 0, len(item.Companions))
		for _, cSel := range item.Companions {
			if cSel.Scheme != selector.Series {
				return nil, fmt.Errorf("trash[%d] companion: expected series: selector, got %q", index, cSel.Scheme)
			}
			cAbs := cSel.Resolve(seriesRoot)
			cRel := cSel.Relative
			if _, dup := seenPaths[cAbs]; dup {
				return nil, &DuplicateBatchPathError{Path: cAbs}
			}
			seenPaths[cAbs] = struct{}{}
			if ep, isActive := activePaths[cAbs]; isActive {
				return nil, &TrashTargetTrackedError{Path: cRel, Episode: ep, RecordKind: "active"}
			}
			if ep, isStaged := stagedPaths[cAbs]; isStaged {
				return nil, &TrashTargetTrackedError{Path: cRel, Episode: ep, RecordKind: "staged"}
			}
			if _, dup := stagedTrashPaths[cAbs]; dup {
				return nil, &TrashAlreadyStagedError{Path: cRel}
			}
			companions = append(companions, cAbs)
			relCompanions = append(relCompanions, cRel)
		}

		out = append(out, resolvedTrashItem{
			TrashStageItem:     item,
			id:                 ulid.Make(),
			resolvedPath:       absPath,
			resolvedCompanions: companions,
			relPath:            relPath,
			relCompanions:      relCompanions,
		})
	}
	return out, nil
}

func validateExtraItems(deps Deps, seriesRoot string, model *domainseries.Series, items []ExtraStageItem) ([]resolvedExtraItem, error) {
	out := make([]resolvedExtraItem, 0, len(items))
	// Build a set of seasons present in the spine for membership check.
	seasons := map[int]struct{}{}
	for ref := range model.Episodes {
		seasons[ref.Season()] = struct{}{}
	}

	// Track per-batch destination collisions.
	seenTargets := map[string]struct{}{}

	for index, item := range items {
		if item.Season < 0 {
			return nil, fmt.Errorf("extras[%d]: season %d is negative", index, item.Season)
		}
		if _, ok := seasons[item.Season]; !ok && item.Season != 0 {
			return nil, &ExtraSeasonMissingError{Season: item.Season}
		}
		if err := validateExtraPrefix(item.Prefix); err != nil {
			return nil, fmt.Errorf("extras[%d]: %w", index, err)
		}
		// Resolve via inbox selector. Extras source must come from
		// inbox; library-internal placement is out of scope.
		absPath, err := resolveInboxSelector(deps.InboxRoot, item.Source)
		if err != nil {
			return nil, fmt.Errorf("extras[%d]: %w", index, err)
		}
		info, err := os.Stat(absPath)
		if err != nil {
			return nil, fmt.Errorf("extras[%d]: %w", index, err)
		}
		isDir := info.IsDir()
		basename := filepath.Base(absPath)
		relTarget := paths.ExtraRel(item.Season, item.Prefix, basename)
		// Per-batch collision.
		if _, dup := seenTargets[relTarget]; dup {
			return nil, &ExtraTargetCollisionError{Path: relTarget}
		}
		seenTargets[relTarget] = struct{}{}
		// On-disk collision.
		absTarget := filepath.Join(seriesRoot, filepath.FromSlash(relTarget))
		if _, err := os.Stat(absTarget); err == nil {
			return nil, &ExtraTargetCollisionError{Path: relTarget}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("extras[%d]: %w", index, err)
		}
		out = append(out, resolvedExtraItem{
			ExtraStageItem: item,
			id:             ulid.Make(),
			resolvedPath:   absPath,
			isDir:          isDir,
			relTarget:      relTarget,
		})
	}
	return out, nil
}

// validateExtraPrefix rejects path-traversal and separator-bearing
// prefixes. Empty is allowed (no sub-folder).
func validateExtraPrefix(prefix string) error {
	if prefix == "" {
		return nil
	}
	if strings.ContainsAny(prefix, "/\\") {
		return &ExtraPrefixInvalidError{Prefix: prefix, Reason: "contains path separator"}
	}
	if prefix == "." || prefix == ".." {
		return &ExtraPrefixInvalidError{Prefix: prefix, Reason: "is a reserved name"}
	}
	if strings.HasPrefix(prefix, ".") {
		return &ExtraPrefixInvalidError{Prefix: prefix, Reason: "starts with '.'"}
	}
	return nil
}

func applyEpisodeItem(ctx context.Context, deps Deps, seriesRoot string, model *domainseries.Series, item resolvedEpisodeItem) (response.StageEpisodeResult, *response.StageSkip, error) {
	// mediainfo probes the resolved abs path (the actual file on disk),
	// but the record we persist carries the selector form so the
	// stored path is portable across container redeploys.
	builderInput := mediainfo.Input{
		MediaPath:  item.resolvedPath,
		RecordPath: item.Media.String(),
		Source:     item.Source,
	}
	for i, c := range item.resolvedCompanions {
		builderInput.CompanionPaths = append(builderInput.CompanionPaths, mediainfo.CompanionInput{
			MediaPath:  c,
			RecordPath: item.resolvedCompanionSels[i].String(),
		})
	}
	record, err := mediainfo.NewBuilder(deps.Inspector).Build(ctx, builderInput)
	if err != nil {
		return response.StageEpisodeResult{}, &response.StageSkip{
			Kind:   "episode",
			Path:   item.Media.String(),
			Code:   "probe_failed",
			Reason: err.Error(),
		}, nil
	}
	replaced := item.episodeStateBefore.Active != nil || item.episodeStateBefore.Staged != nil
	if err := model.SetStaged(item.Episode, record); err != nil {
		return response.StageEpisodeResult{}, nil, err
	}
	status := "staged"
	if replaced {
		status = "replaced"
	}
	return response.StageEpisodeResult{
		Episode: item.Episode,
		Status:  status,
		Record:  mediaShow(seriesRoot, record),
	}, nil, nil
}

func applyTrashItem(model *domainseries.Series, item resolvedTrashItem, now time.Time) (response.StageTrashResult, *response.StageSkip, error) {
	info, err := os.Stat(item.resolvedPath)
	if err != nil {
		return response.StageTrashResult{}, &response.StageSkip{
			Kind:   "trash",
			Path:   item.resolvedPath,
			Code:   "stat_failed",
			Reason: err.Error(),
		}, nil
	}
	companions := make([]media.Companion, 0, len(item.resolvedCompanions))
	for _, cAbs := range item.resolvedCompanions {
		cInfo, statErr := os.Stat(cAbs)
		if statErr != nil {
			return response.StageTrashResult{}, &response.StageSkip{
				Kind:   "trash",
				Path:   cAbs,
				Code:   "stat_failed",
				Reason: statErr.Error(),
			}, nil
		}
		companions = append(companions, media.Companion{
			Path:  cAbs,
			Size:  cInfo.Size(),
			MTime: cInfo.ModTime().UTC().Truncate(time.Second),
		})
	}
	model.AddStagedTrash(domainseries.StagedTrashItem{
		ID:         item.id,
		Path:       item.resolvedPath,
		Size:       info.Size(),
		MTime:      info.ModTime().UTC().Truncate(time.Second),
		AddedAt:    now,
		Companions: companions,
	})
	return response.StageTrashResult{
		ID:   item.id.String(),
		Path: item.relPath,
	}, nil, nil
}

func applyExtraItem(model *domainseries.Series, item resolvedExtraItem, now time.Time) (response.StageExtraResult, *response.StageSkip, error) {
	// Persisted Path carries the selector form so reconcile can
	// re-resolve via deps.InboxRoot at apply time.
	model.AddStagedExtra(domainseries.StagedExtraItem{
		ID:      item.id,
		Season:  item.Season,
		Path:    item.Source.String(),
		Prefix:  item.Prefix,
		IsDir:   item.isDir,
		AddedAt: now,
	})
	return response.StageExtraResult{
		ID:     item.id.String(),
		Season: item.Season,
		Path:   item.Source.String(),
		Prefix: item.Prefix,
	}, nil, nil
}
