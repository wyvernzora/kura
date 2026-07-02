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
	"github.com/wyvernzora/kura/internal/textnorm"
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

// EpisodeStageItem queues one episode stage. Media accepts two
// selector schemes:
//
//   - inbox:<rel> — normal staging from the inbox root. Resolves via
//     deps.InboxRoot. Companions follow the same shape.
//   - series:<rel> — in-place metadata override on the existing
//     active record. The resolved path MUST equal the episode's
//     active record path; Replace MUST be true; Companions MUST be
//     empty (existing companions are preserved). Used to fix
//     wrong-source / wrong-resolution facts on a recorded file
//     without moving it back through the inbox.
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

	out.Episodes, out.Skipped, err = applyEpisodeItemsLoop(ctx, deps, seriesRoot, model, episodes, totalItems, out.Skipped)
	if err != nil {
		return response.StageResult{}, err
	}
	out.Trash, out.Skipped, err = applyTrashItemsLoop(ctx, model, trash, len(episodes), totalItems, now, out.Skipped)
	if err != nil {
		return response.StageResult{}, err
	}
	out.Extras, out.Skipped, err = applyExtraItemsLoop(ctx, model, extras, len(episodes)+len(trash), totalItems, now, out.Skipped)
	if err != nil {
		return response.StageResult{}, err
	}

	if err := seriesfile.SaveCAS(deps.LibRoot, model, coord.NewMutator("stage")); err != nil {
		return response.StageResult{}, err
	}
	if err := updateIndexModel(ctx, deps, model, "stage"); err != nil {
		return response.StageResult{}, err
	}

	progress.Success(ctx, "stage",
		fmt.Sprintf("Staged %d ep + %d trash + %d extras (%d skipped) for %s",
			len(out.Episodes), len(out.Trash), len(out.Extras), len(out.Skipped), in.Ref),
		totalItems)
	succeeded = true
	return out, nil
}

// applyEpisodeItemsLoop walks the validated episode batch and forwards
// each to applyEpisodeItem, partitioning results into the applied
// + skipped buckets. Skipped items append to the skipped slice the
// caller already accumulated for the prior axes.
func applyEpisodeItemsLoop(
	ctx context.Context,
	deps Deps,
	seriesRoot string,
	model *domainseries.Series,
	items []resolvedEpisodeItem,
	totalItems int,
	skipped []response.StageSkip,
) ([]response.StageEpisodeResult, []response.StageSkip, error) {
	applied := make([]response.StageEpisodeResult, 0, len(items))
	for index, item := range items {
		progress.Update(ctx, "stage", fmt.Sprintf("Inspecting %s", filepath.Base(item.resolvedPath)), index+1, totalItems)
		result, skip, err := applyEpisodeItem(ctx, deps, seriesRoot, model, item)
		if err != nil {
			return nil, nil, err
		}
		if skip != nil {
			skipped = append(skipped, *skip)
			continue
		}
		applied = append(applied, result)
	}
	return applied, skipped, nil
}

// applyTrashItemsLoop is the trash counterpart to applyEpisodeItemsLoop.
// `progressOffset` is the count of items already processed by earlier
// axes so the live progress counter stays continuous across the batch.
func applyTrashItemsLoop(
	ctx context.Context,
	model *domainseries.Series,
	items []resolvedTrashItem,
	progressOffset, totalItems int,
	now time.Time,
	skipped []response.StageSkip,
) ([]response.StageTrashResult, []response.StageSkip, error) {
	applied := make([]response.StageTrashResult, 0, len(items))
	for index, item := range items {
		progress.Update(ctx, "stage", fmt.Sprintf("Queuing trash %s", filepath.Base(item.resolvedPath)), progressOffset+index+1, totalItems)
		result, skip, err := applyTrashItem(model, item, now)
		if err != nil {
			return nil, nil, err
		}
		if skip != nil {
			skipped = append(skipped, *skip)
			continue
		}
		applied = append(applied, result)
	}
	return applied, skipped, nil
}

// applyExtraItemsLoop is the extras counterpart to
// applyEpisodeItemsLoop / applyTrashItemsLoop.
func applyExtraItemsLoop(
	ctx context.Context,
	model *domainseries.Series,
	items []resolvedExtraItem,
	progressOffset, totalItems int,
	now time.Time,
	skipped []response.StageSkip,
) ([]response.StageExtraResult, []response.StageSkip, error) {
	applied := make([]response.StageExtraResult, 0, len(items))
	for index, item := range items {
		progress.Update(ctx, "stage", fmt.Sprintf("Queuing extra %s", filepath.Base(item.resolvedPath)), progressOffset+index+1, totalItems)
		result, skip, err := applyExtraItem(model, item, now)
		if err != nil {
			return nil, nil, err
		}
		if skip != nil {
			skipped = append(skipped, *skip)
			continue
		}
		applied = append(applied, result)
	}
	return applied, skipped, nil
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

// resolveSeriesSelector validates a series: selector, resolves it
// under seriesRoot, and confirms the result stays within the series
// directory (catches symlink escapes). Mirrors resolveInboxSelector.
func resolveSeriesSelector(seriesRoot string, sel selector.Path) (string, error) {
	if sel.IsZero() {
		return "", errors.New("series selector is empty")
	}
	abs := sel.Resolve(seriesRoot)
	rootAbs, err := filepath.Abs(seriesRoot)
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
		resolved, err := validateOneEpisodeItem(deps, model, item, index)
		if err != nil {
			return nil, err
		}
		out = append(out, resolved)
	}
	return out, nil
}

// validateOneEpisodeItem runs all per-item validation for one episode
// stage entry: selector resolution, file-shape gates, episode-state
// gates, and companion resolution. Returns the resolved record on
// success or a typed/wrapped error.
func validateOneEpisodeItem(
	deps Deps,
	model *domainseries.Series,
	item EpisodeStageItem,
	index int,
) (resolvedEpisodeItem, error) {
	episodeState, ok := model.Episodes[item.Episode]
	if !ok {
		return resolvedEpisodeItem{}, &MetadataMissingEpisodeError{Episode: item.Episode}
	}
	seriesRoot := paths.SeriesDir(deps.LibRoot, model.Ref)
	mediaPath, err := resolveStageMediaSelector(deps.InboxRoot, seriesRoot, item.Media, index)
	if err != nil {
		return resolvedEpisodeItem{}, err
	}
	if err := gateMediaFile(mediaPath, item.Media, index); err != nil {
		return resolvedEpisodeItem{}, err
	}
	// In-place override: series: media targeting THIS episode's active
	// record. Companions are preserved from the active record; replace
	// is required because the active record's facts will be replaced.
	if item.Media.Scheme == selector.Series && episodeState.Active != nil && pathsEquivalentNFC(mediaPath, episodeState.Active.Path) {
		return validateInPlaceOverride(seriesRoot, item, episodeState, mediaPath, index)
	}
	return validateNormalEpisodeItem(deps, seriesRoot, model, item, episodeState, mediaPath, index)
}

// resolveStageMediaSelector dispatches to the inbox or series resolver
// based on the selector scheme. Other schemes are rejected.
func resolveStageMediaSelector(inboxRoot, seriesRoot string, sel selector.Path, index int) (string, error) {
	switch sel.Scheme {
	case selector.Inbox:
		abs, err := resolveInboxSelector(inboxRoot, sel)
		if err != nil {
			return "", fmt.Errorf("episodes[%d]: %w", index, err)
		}
		return abs, nil
	case selector.Series:
		abs, err := resolveSeriesSelector(seriesRoot, sel)
		if err != nil {
			return "", fmt.Errorf("episodes[%d]: %w", index, err)
		}
		return abs, nil
	default:
		return "", fmt.Errorf("episodes[%d]: expected inbox: or series: media selector, got %q", index, sel.Scheme)
	}
}

// validateNormalEpisodeItem handles every stage path that isn't an
// in-place override: standard inbox: stages, and series: stages whose
// path is NOT the current episode's active record. For series: stages
// the media (and each companion) must not overlap any active or staged
// record path or companion path tracked elsewhere in the series.
func validateNormalEpisodeItem(
	deps Deps,
	seriesRoot string,
	model *domainseries.Series,
	item EpisodeStageItem,
	episodeState domainseries.Episode,
	mediaPath string,
	index int,
) (resolvedEpisodeItem, error) {
	if episodeState.Active != nil && !item.Replace {
		if _, statErr := os.Stat(episodeState.Active.Path); statErr == nil {
			return resolvedEpisodeItem{}, &EpisodeAlreadyExistsError{Episode: item.Episode}
		}
	}
	if episodeState.Staged != nil && !item.Replace {
		return resolvedEpisodeItem{}, &StagedEpisodeAlreadyExistsError{Episode: item.Episode}
	}
	var (
		companions    []string
		companionSels []selector.Path
		err           error
	)
	if item.Media.Scheme == selector.Series {
		claimed := buildClaimedPathsSet(model, deps.InboxRoot, seriesRoot)
		if _, hit := claimed[mediaPath]; hit {
			return resolvedEpisodeItem{}, fmt.Errorf("episodes[%d]: media %q is already an active or staged record path or companion in this series; reset the existing entry or pick a different file", index, item.Media)
		}
		companions, companionSels, err = validateSeriesCompanions(seriesRoot, item.Companions, index)
		if err != nil {
			return resolvedEpisodeItem{}, err
		}
		for i, abs := range companions {
			if _, hit := claimed[abs]; hit {
				return resolvedEpisodeItem{}, fmt.Errorf("episodes[%d]: companion %q is already an active or staged record path or companion in this series", index, item.Companions[i])
			}
		}
	} else {
		companions, companionSels, err = validateCompanions(deps, item.Companions, index)
		if err != nil {
			return resolvedEpisodeItem{}, err
		}
	}
	return resolvedEpisodeItem{
		EpisodeStageItem:      item,
		resolvedPath:          mediaPath,
		resolvedCompanions:    companions,
		resolvedCompanionSels: companionSels,
		episodeStateBefore:    episodeState,
	}, nil
}

// validateInPlaceOverride covers the special case where a series:
// media stage points at the current episode's own active record. The
// active record's companions are preserved verbatim; user-supplied
// companions are forbidden so the override stays a metadata-only fix.
func validateInPlaceOverride(
	seriesRoot string,
	item EpisodeStageItem,
	episodeState domainseries.Episode,
	mediaPath string,
	index int,
) (resolvedEpisodeItem, error) {
	if !item.Replace {
		return resolvedEpisodeItem{}, fmt.Errorf("episodes[%d]: series: media (in-place override) requires replace=true", index)
	}
	if len(item.Companions) > 0 {
		return resolvedEpisodeItem{}, fmt.Errorf("episodes[%d]: series: media targeting the active record must omit companions; existing companions are preserved", index)
	}
	if episodeState.Staged != nil {
		return resolvedEpisodeItem{}, &StagedEpisodeAlreadyExistsError{Episode: item.Episode}
	}
	companions := make([]string, 0, len(episodeState.Active.Companions))
	companionSels := make([]selector.Path, 0, len(episodeState.Active.Companions))
	for _, c := range episodeState.Active.Companions {
		companions = append(companions, c.Path)
		rel, err := filepath.Rel(seriesRoot, c.Path)
		if err != nil {
			return resolvedEpisodeItem{}, fmt.Errorf("episodes[%d]: companion %q outside series root: %w", index, c.Path, err)
		}
		cleaned, cleanErr := selector.CleanRelative(filepath.ToSlash(rel))
		if cleanErr != nil {
			return resolvedEpisodeItem{}, fmt.Errorf("episodes[%d]: companion %q: %w", index, c.Path, cleanErr)
		}
		companionSels = append(companionSels, selector.Path{Scheme: selector.Series, Relative: cleaned})
	}
	return resolvedEpisodeItem{
		EpisodeStageItem:      item,
		resolvedPath:          mediaPath,
		resolvedCompanions:    companions,
		resolvedCompanionSels: companionSels,
		episodeStateBefore:    episodeState,
	}, nil
}

// buildClaimedPathsSet returns the set of absolute paths that are
// currently claimed by any active or staged record (or any of their
// companions) in the series model. Used to reject series: stages that
// would alias existing tracked files. Inbox-resident staged paths are
// resolved under inboxRoot; series-resident staged + active paths
// resolve under seriesRoot.
func buildClaimedPathsSet(model *domainseries.Series, inboxRoot, seriesRoot string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, ep := range model.Episodes {
		if ep.Active != nil {
			out[ep.Active.Path] = struct{}{}
			for _, c := range ep.Active.Companions {
				out[c.Path] = struct{}{}
			}
		}
		if ep.Staged != nil {
			if abs := resolveTrackedPath(ep.Staged.Path, inboxRoot, seriesRoot); abs != "" {
				out[abs] = struct{}{}
			}
			for _, c := range ep.Staged.Companions {
				if abs := resolveTrackedPath(c.Path, inboxRoot, seriesRoot); abs != "" {
					out[abs] = struct{}{}
				}
			}
		}
	}
	return out
}

// resolveTrackedPath returns the absolute filesystem path for a value
// pulled from a persisted record (Active.Path, Staged.Path, or any
// companion Path field). Active records are absolutized at Load so
// they pass through unchanged. Staged records carry a selector form
// (inbox: or series:) which we resolve under the matching root.
// Returns "" when the value can't be interpreted.
func resolveTrackedPath(raw, inboxRoot, seriesRoot string) string {
	if filepath.IsAbs(raw) {
		return raw
	}
	sel, err := selector.Parse(raw)
	if err != nil {
		return ""
	}
	switch sel.Scheme {
	case selector.Inbox:
		return sel.Resolve(inboxRoot)
	case selector.Series:
		return sel.Resolve(seriesRoot)
	}
	return ""
}

// validateSeriesCompanions resolves and shape-checks each companion
// selector for an episode whose media is series:-scheme. Mirrors
// validateCompanions but with selector.Series as the required scheme
// and seriesRoot as the resolution root.
func validateSeriesCompanions(seriesRoot string, sels []selector.Path, index int) ([]string, []selector.Path, error) {
	companions := make([]string, 0, len(sels))
	companionSels := make([]selector.Path, 0, len(sels))
	for _, sel := range sels {
		if sel.Scheme != selector.Series {
			return nil, nil, fmt.Errorf("episodes[%d]: companion %q must be a series: selector when media is series:", index, sel)
		}
		abs, err := resolveSeriesSelector(seriesRoot, sel)
		if err != nil {
			return nil, nil, fmt.Errorf("episodes[%d]: %w", index, err)
		}
		companions = append(companions, abs)
		companionSels = append(companionSels, sel)
	}
	return companions, companionSels, nil
}

// gateMediaFile lstats the resolved abs path and rejects directories
// and unrecognized extensions. Shared between the inbox and series
// validation paths.
func gateMediaFile(abs string, sel selector.Path, index int) error {
	info, err := os.Lstat(abs)
	if err != nil {
		return fmt.Errorf("episodes[%d]: %w", index, err)
	}
	if info.IsDir() {
		return fmt.Errorf("episodes[%d]: %q is a directory", index, sel)
	}
	if !mediainfo.RecognizedVideoFile(abs) {
		return fmt.Errorf("episodes[%d]: %q is not a recognized video file", index, sel)
	}
	return nil
}

// pathsEquivalentNFC mirrors the reconcile helper. Cheap NFC-normalized
// equality so SMB/AFP-decomposed basenames compare cleanly.
func pathsEquivalentNFC(a, b string) bool {
	if a == b {
		return true
	}
	return textnorm.NFC(a).String() == textnorm.NFC(b).String()
}

// validateCompanions resolves and shape-checks each companion selector
// for one episode stage entry. Returns the parallel slices of resolved
// abs paths and the original selectors (preserved for persistence).
func validateCompanions(deps Deps, sels []selector.Path, index int) ([]string, []selector.Path, error) {
	companions := make([]string, 0, len(sels))
	companionSels := make([]selector.Path, 0, len(sels))
	for _, sel := range sels {
		c, err := resolveInboxSelector(deps.InboxRoot, sel)
		if err != nil {
			return nil, nil, fmt.Errorf("episodes[%d] companion: %w", index, err)
		}
		cInfo, statErr := os.Lstat(c)
		if statErr != nil {
			return nil, nil, fmt.Errorf("episodes[%d] companion: %w", index, statErr)
		}
		if cInfo.IsDir() {
			return nil, nil, fmt.Errorf("episodes[%d] companion %q is a directory", index, sel)
		}
		companions = append(companions, c)
		companionSels = append(companionSels, sel)
	}
	return companions, companionSels, nil
}

// trashLookupSets bundles the active / staged / staged-trash path
// indexes used by the trash invariant checks.
type trashLookupSets struct {
	activePaths      map[string]domainrefs.Episode
	stagedPaths      map[string]domainrefs.Episode
	stagedTrashPaths map[string]struct{}
}

// buildTrashLookupSets folds the series model into the path indexes
// validateTrashItems needs to enforce the four trash invariants.
func buildTrashLookupSets(model *domainseries.Series) trashLookupSets {
	sets := trashLookupSets{
		activePaths:      map[string]domainrefs.Episode{},
		stagedPaths:      map[string]domainrefs.Episode{},
		stagedTrashPaths: map[string]struct{}{},
	}
	for ref, ep := range model.Episodes {
		if ep.Active != nil {
			sets.activePaths[ep.Active.Path] = ref
			for _, c := range ep.Active.Companions {
				sets.activePaths[c.Path] = ref
			}
		}
		if ep.Staged != nil {
			sets.stagedPaths[ep.Staged.Path] = ref
			for _, c := range ep.Staged.Companions {
				sets.stagedPaths[c.Path] = ref
			}
		}
	}
	for _, t := range model.StagedTrash {
		sets.stagedTrashPaths[t.Path] = struct{}{}
		for _, c := range t.Companions {
			sets.stagedTrashPaths[c.Path] = struct{}{}
		}
	}
	return sets
}

// checkTrashPath enforces the three trash invariants (not active / not
// staged / not already in stagedTrash) for a single resolved path.
// relPath is used for error messages so the operator sees the wire
// form they typed.
func checkTrashPath(absPath, relPath string, sets trashLookupSets) error {
	if ep, isActive := sets.activePaths[absPath]; isActive {
		return &TrashTargetTrackedError{Path: relPath, Episode: ep, RecordKind: "active"}
	}
	if ep, isStaged := sets.stagedPaths[absPath]; isStaged {
		return &TrashTargetTrackedError{Path: relPath, Episode: ep, RecordKind: "staged"}
	}
	if _, dup := sets.stagedTrashPaths[absPath]; dup {
		return &TrashAlreadyStagedError{Path: relPath}
	}
	return nil
}

// validateOneTrashItem runs full validation for one trash entry: the
// scheme + invariant checks on the primary path, then the same on each
// companion. seenPaths is mutated in place to track per-batch dedup
// across primary + companions across all items.
func validateOneTrashItem(
	seriesRoot string,
	item TrashStageItem,
	index int,
	sets trashLookupSets,
	seenPaths map[string]struct{},
) (resolvedTrashItem, error) {
	if item.Path.IsZero() {
		return resolvedTrashItem{}, fmt.Errorf("trash[%d]: path selector is empty", index)
	}
	if item.Path.Scheme != selector.Series {
		return resolvedTrashItem{}, fmt.Errorf("trash[%d]: expected series: selector, got %q", index, item.Path.Scheme)
	}
	absPath := item.Path.Resolve(seriesRoot)
	relPath := item.Path.Relative
	if _, dup := seenPaths[absPath]; dup {
		return resolvedTrashItem{}, &DuplicateBatchPathError{Path: absPath}
	}
	seenPaths[absPath] = struct{}{}
	if err := checkTrashPath(absPath, relPath, sets); err != nil {
		return resolvedTrashItem{}, err
	}

	companions := make([]string, 0, len(item.Companions))
	relCompanions := make([]string, 0, len(item.Companions))
	for _, cSel := range item.Companions {
		if cSel.Scheme != selector.Series {
			return resolvedTrashItem{}, fmt.Errorf("trash[%d] companion: expected series: selector, got %q", index, cSel.Scheme)
		}
		cAbs := cSel.Resolve(seriesRoot)
		cRel := cSel.Relative
		if _, dup := seenPaths[cAbs]; dup {
			return resolvedTrashItem{}, &DuplicateBatchPathError{Path: cAbs}
		}
		seenPaths[cAbs] = struct{}{}
		if err := checkTrashPath(cAbs, cRel, sets); err != nil {
			return resolvedTrashItem{}, err
		}
		companions = append(companions, cAbs)
		relCompanions = append(relCompanions, cRel)
	}

	return resolvedTrashItem{
		TrashStageItem:     item,
		id:                 ulid.Make(),
		resolvedPath:       absPath,
		resolvedCompanions: companions,
		relPath:            relPath,
		relCompanions:      relCompanions,
	}, nil
}

// validateTrashItems enforces the four trash invariants in addition to
// selector resolution and per-batch dedup. Trash inputs are series:
// selectors scoped to seriesRoot (the request's series ref).
func validateTrashItems(seriesRoot string, model *domainseries.Series, items []TrashStageItem) ([]resolvedTrashItem, error) {
	out := make([]resolvedTrashItem, 0, len(items))
	seenPaths := map[string]struct{}{}
	sets := buildTrashLookupSets(model)
	for index, item := range items {
		resolved, err := validateOneTrashItem(seriesRoot, item, index, sets, seenPaths)
		if err != nil {
			return nil, err
		}
		out = append(out, resolved)
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
			Path:   seriesSelector("", item.relPath),
			Code:   "stat_failed",
			Reason: err.Error(),
		}, nil
	}
	companions := make([]media.Companion, 0, len(item.resolvedCompanions))
	for cIdx, cAbs := range item.resolvedCompanions {
		cInfo, statErr := os.Stat(cAbs)
		if statErr != nil {
			return response.StageTrashResult{}, &response.StageSkip{
				Kind:   "trash",
				Path:   seriesSelector("", item.relCompanions[cIdx]),
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
		Path: seriesSelector("", item.relPath),
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
