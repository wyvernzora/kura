package reconcile

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/services/library/internal/coord"
	"github.com/wyvernzora/kura/services/library/internal/domain/media"
	"github.com/wyvernzora/kura/services/library/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library/internal/domain/selector"
	domainseries "github.com/wyvernzora/kura/services/library/internal/domain/series"
	"github.com/wyvernzora/kura/services/library/internal/fsop"
	"github.com/wyvernzora/kura/services/library/internal/progress"
	"github.com/wyvernzora/kura/services/library/internal/storage/paths"
	"github.com/wyvernzora/kura/services/library/internal/storage/seriesdir"
	"github.com/wyvernzora/kura/services/library/internal/storage/seriesfile"
	"github.com/wyvernzora/kura/services/library/internal/storage/trashfile"
)

// LogWriter is the callbacks Apply uses to append events / result to
// the persisted plan log. Implemented by planfile after the v1→v2
// migration; injected via ApplyInput so this package doesn't import
// planfile (and so tests can stub it).
type LogWriter interface {
	AppendEvent(at time.Time, stepID string, err error) error
	AppendResult(at time.Time, status string, applied int, err error) error
}

// ApplyInput parameters for the Apply entry point.
//
// Plan is the pre-loaded v2 plan; the caller (workflow shim) reads it
// from planfile, validates plan-applied, opens a log, and hands both
// to Apply. Reconcile re-validates the snapshot inside the goroutine.
type ApplyInput struct {
	Ref     refs.Series
	Plan    Plan
	Log     LogWriter
	LogStop func() error // closes the log when Apply finishes
}

// ApplyResult carries the outcome of an Apply invocation, populated on
// both success and failure. AppliedStepIDs lists step IDs in execution
// order; FailedStep is non-nil only when a step-execution failure
// triggered the error return. Pre-flight failures (snapshot stale,
// claim contention) leave FailedStep nil — those did not touch any
// step.
//
// On partial failure, series.json reflects pre-apply state — post-state
// mutations (staged → active promotion, stagedTrash drain, stagedExtras
// drain) only run when every step succeeds. Operator's recovery path is
// `kura reconcile recover` + `kura scan`.
type ApplyResult struct {
	Series         refs.Series
	AppliedSteps   int
	TotalSteps     int
	AppliedStepIDs []string
	FailedStep     *FailedStepRef
}

// FailedStepRef identifies the step whose execution failed. Owner
// captures the kind (episode / trash / extra) so callers can render
// "episode S01E03 failed" without re-walking the plan; details for
// trash steps stay coarse on agent surfaces (see MCP projection) but
// are full-fidelity for operator logs.
type FailedStepRef struct {
	ID         string
	Kind       StepKind
	OwnerKind  OwnerKind
	From       string
	To         string
	Path       string
	ErrMessage string
}

// Apply executes the plan: acquire claim, write trashfile.Meta per
// trash step, iterate steps in order (file_move / dir_remove), append
// per-step events, then post-apply state + SaveCAS + claim release.
// Wrapped in coord.WithSeries (no retry) since side effects are not
// safely re-runnable.
//
// Apply does NOT spawn a job; the workflow shim is responsible for
// jobs.Submit. Apply runs synchronously inside whatever goroutine
// invokes it.
func Apply(ctx context.Context, deps Deps, in ApplyInput) (ApplyResult, error) {
	var out ApplyResult
	var inner error
	err := deps.Coordinator.WithSeries(ctx, in.Ref, func() error {
		result, runErr := applyLocked(ctx, deps, in)
		// Capture the populated result regardless of outcome so the
		// caller sees partial-progress on failure.
		out = result
		inner = runErr
		return runErr
	})
	if err != nil {
		// Coordinator returned the inner error verbatim; prefer it so
		// typed errors (ApplyStepError, StaleSnapshotError, etc.)
		// reach the caller. If the coordinator added its own wrap
		// (e.g. BusyError before applyLocked ran), inner is nil and
		// out is the zero value — return err as-is.
		if inner != nil {
			return out, inner
		}
		return out, err
	}
	return out, nil
}

// recordFailure appends a "failure" result to the JSONL apply log.
// If the append itself fails (disk full, EIO, peer truncation), the
// failure is surfaced via slog so an operator inspecting the audit
// stream still has a trail back to the cause. Caller is responsible
// for returning the underlying err — this helper only handles the
// log-side bookkeeping.
func recordFailure(log *slog.Logger, w LogWriter, at time.Time, applied int, err error) {
	if appendErr := w.AppendResult(at, "failure", applied, err); appendErr != nil {
		log.Error("apply log append failed",
			"applied", applied,
			"appendErr", appendErr,
			"err", err,
		)
	}
}

func applyLocked(ctx context.Context, deps Deps, in ApplyInput) (ApplyResult, error) {
	defer func() {
		if in.LogStop != nil {
			_ = in.LogStop()
		}
	}()

	total := len(in.Plan.Steps)
	base := ApplyResult{Series: in.Ref, TotalSteps: total}
	log := deps.log().With("ref", in.Ref.String(), "token", in.Plan.Header.Token)
	startedAt := deps.Now()
	log.Info("apply starting", "totalSteps", total)

	if err := validateApplyPreflight(deps, in, log); err != nil {
		return base, err
	}
	if !in.Plan.HasWork() {
		if err := in.Log.AppendResult(deps.Now(), "success", 0, nil); err != nil {
			return base, err
		}
		log.Info("apply complete", "appliedSteps", 0, "totalSteps", 0, "duration", deps.Now().Sub(startedAt))
		return base, nil
	}
	log.Debug("apply pre-flight ok")

	seriesDir, err := seriesdir.Parse(paths.SeriesDir(deps.LibRoot, in.Ref))
	if err != nil {
		recordFailure(log, in.Log, deps.Now(), 0, err)
		return base, err
	}
	preLoaded, err := seriesfile.Load(deps.LibRoot, in.Ref)
	if err != nil {
		recordFailure(log, in.Log, deps.Now(), 0, err)
		return base, err
	}
	holder, claimErr := acquireClaim(deps, preLoaded, in.Plan.Header.Token)
	if claimErr != nil {
		recordFailure(log, in.Log, deps.Now(), 0, claimErr)
		return base, claimErr
	}
	claimedHash := preLoaded.Hash
	released := false
	defer func() {
		if released {
			return
		}
		if err := releaseClaim(deps, in.Ref, holder, claimedHash); err != nil {
			log.Error("apply claim release failed", "err", err)
		}
	}()

	exec := &executor{
		deps:      deps,
		in:        in,
		seriesDir: seriesDir.Path(),
		holder:    holder,
		log:       log,
	}
	if err := exec.writeTrashMetas(); err != nil {
		recordFailure(log, in.Log, deps.Now(), 0, err)
		log.Error("apply failed", "phase", "trash-meta", "err", err)
		return base, err
	}
	log.Debug("apply trash metas written")

	applied, failedStep, runErr := exec.runSteps(ctx)
	if runErr != nil {
		recordFailure(log, in.Log, deps.Now(), len(applied), runErr)
		logApplyFailure(log, total, applied, failedStep, runErr)
		return buildFailedApplyResult(base, applied, failedStep, runErr), runErr
	}

	if err := finalizeApply(ctx, deps, in, exec, holder, applied, &released); err != nil {
		return buildAppliedResult(base, applied), err
	}
	log.Info("apply complete",
		"appliedSteps", len(applied),
		"totalSteps", total,
		"duration", deps.Now().Sub(startedAt),
	)
	return buildAppliedResult(base, applied), nil
}

// validateApplyPreflight enforces the plan invariants apply expects to
// hold before any CAS / I/O: the plan's series matches the apply
// target, and the post-plan disk snapshot still matches what the
// planner saw. recordFailure is invoked for non-stale-snapshot
// failures (StaleSnapshot is logged by the caller — the plan is
// unrecoverable, no apply log to write to).
func validateApplyPreflight(deps Deps, in ApplyInput, log *slog.Logger) error {
	if in.Plan.Header.Series != in.Ref {
		return StaleSnapshotError{Series: in.Plan.Header.Series}
	}
	if err := validateAppliedSnapshot(deps.LibRoot, in.Ref, in.Plan); err != nil {
		recordFailure(log, in.Log, deps.Now(), 0, err)
		return err
	}
	return nil
}

// finalizeApply runs the post-runSteps phase: re-load the series, check
// the claim wasn't stolen, apply post-state mutations, persist, update
// the index, mark the claim as released, and append the success entry
// to the apply log. Sets *released=true between SaveCAS and the final
// AppendResult so the caller's defer skips the release call once the
// claim has been cleared in series.json.
func finalizeApply(
	ctx context.Context,
	deps Deps,
	in ApplyInput,
	exec *executor,
	holder coord.Holder,
	applied []string,
	released *bool,
) error {
	postLoaded, err := seriesfile.Load(deps.LibRoot, in.Ref)
	if err != nil {
		recordFailure(exec.log, in.Log, deps.Now(), len(applied), err)
		return err
	}
	if !claimMatches(postLoaded.InProgress, holder) {
		stolen := &coord.ClaimStolenError{Scope: coord.SeriesScope(in.Ref), Expected: holder, Found: postLoaded.InProgress}
		recordFailure(exec.log, in.Log, deps.Now(), len(applied), stolen)
		return stolen
	}
	if err := exec.applyPostStateMutations(postLoaded); err != nil {
		recordFailure(exec.log, in.Log, deps.Now(), len(applied), err)
		exec.log.Error("apply failed", "phase", "post-state", "appliedSteps", len(applied), "err", err)
		return err
	}
	exec.log.Debug("apply post-state mutations applied")
	postLoaded.InProgress = nil
	if err := seriesfile.SaveCAS(deps.LibRoot, postLoaded, coord.NewMutator("reconcile_apply")); err != nil {
		recordFailure(exec.log, in.Log, deps.Now(), len(applied), err)
		return err
	}
	if deps.UpdateIndex != nil {
		if err := deps.UpdateIndex(ctx, postLoaded, "reconcile_apply"); err != nil {
			recordFailure(exec.log, in.Log, deps.Now(), len(applied), err)
			return err
		}
	}
	*released = true
	exec.log.Debug("apply releasing claim")
	if err := in.Log.AppendResult(deps.Now(), "success", len(applied), nil); err != nil {
		return err
	}
	return nil
}

// buildAppliedResult composes the success-side ApplyResult from the
// caller's base + the applied step IDs. Used by both the
// finalizeApply success path and the post-finalize-error path so the
// response always carries the steps the executor managed to land.
func buildAppliedResult(base ApplyResult, applied []string) ApplyResult {
	out := base
	out.AppliedSteps = len(applied)
	if len(applied) > 0 {
		out.AppliedStepIDs = append([]string(nil), applied...)
	}
	return out
}

// buildFailedApplyResult composes the runSteps-error ApplyResult,
// including the FailedStepRef when the executor reported a specific
// step at fault.
func buildFailedApplyResult(base ApplyResult, applied []string, failedStep *Step, runErr error) ApplyResult {
	out := buildAppliedResult(base, applied)
	if failedStep != nil {
		out.FailedStep = &FailedStepRef{
			ID:         failedStep.ID,
			Kind:       failedStep.Kind,
			OwnerKind:  failedStep.Owner.Kind,
			From:       failedStep.From,
			To:         failedStep.To,
			Path:       failedStep.Path,
			ErrMessage: runErr.Error(),
		}
	}
	return out
}

// logApplyFailure emits the structured "apply failed" line. With a
// failedStep the line carries the step's id/kind/owner; without one,
// only the running totals + the underlying error.
func logApplyFailure(log *slog.Logger, total int, applied []string, failedStep *Step, runErr error) {
	if failedStep != nil {
		log.Error("apply failed",
			"appliedSteps", len(applied),
			"totalSteps", total,
			"failedStepID", failedStep.ID,
			"failedKind", string(failedStep.Kind),
			"failedOwner", string(failedStep.Owner.Kind),
			"err", runErr,
		)
		return
	}
	log.Error("apply failed",
		"appliedSteps", len(applied),
		"totalSteps", total,
		"err", runErr,
	)
}

// executor bundles the per-Apply state. Methods close over the bundle.
type executor struct {
	deps      Deps
	in        ApplyInput
	seriesDir string
	holder    coord.Holder
	log       *slog.Logger
}

// writeTrashMetas writes one trashfile.Meta per trash step before any
// move runs. A crash mid-move leaves self-describing entries rather
// than orphan files in a ULID dir without metadata. Idempotent across
// retries (renameio.WriteFile overwrites whatever was there).
func (e *executor) writeTrashMetas() error {
	for _, step := range e.in.Plan.Steps {
		if step.Owner.Kind != OwnerTrash {
			continue
		}
		// Each trash bucket gets one meta.json. The primary file_move
		// step (the first one for the bucket, given plan ordering) is
		// the one that triggers the meta write. Companion file_move
		// steps for the same bucket already had their data folded
		// into the bucket's owner.Record at plan time.
		if !isPrimaryTrashStep(e.in.Plan.Steps, step) {
			continue
		}
		if step.Owner.Record == nil {
			return fmt.Errorf("reconcile: trash step %s missing owner.Record", step.ID)
		}
		id, err := ulid.Parse(step.Owner.TrashID)
		if err != nil {
			return fmt.Errorf("reconcile: parse trash ulid %q: %w", step.Owner.TrashID, err)
		}
		meta := trashfile.Meta{
			ID:        id,
			Episode:   step.Owner.OriginalEpisode,
			TrashedAt: e.deps.Now().UTC(),
			Record:    recordToTrashfile(step.Owner.Record, step.From),
		}
		if err := trashfile.Write(e.deps.LibRoot, e.in.Ref, meta); err != nil {
			return fmt.Errorf("reconcile: write trash meta for %s: %w", id, err)
		}
	}
	return nil
}

// isPrimaryTrashStep reports whether step is the first step in the plan
// with this TrashID — the "primary" media move that anchors the bucket
// (companion moves follow). Used to dedup meta writes per bucket.
func isPrimaryTrashStep(all []Step, step Step) bool {
	for _, s := range all {
		if s.Owner.Kind == OwnerTrash && s.Owner.TrashID == step.Owner.TrashID {
			return s.ID == step.ID
		}
	}
	return false
}

// recordToTrashfile translates the inlined owner.Record into the
// trashfile.Record shape, using the pre-move series-relative path
// (step.From) as the recorded Path so TrashRestore can compute the
// original location.
//
// step.From may carry the series: scheme; trashfile stores bare
// series-relative paths (TrashRestore joins them with seriesRoot
// directly), so we strip the prefix here.
func recordToTrashfile(rec *ReplacedRecord, postMovePath string) trashfile.Record {
	if sel, err := selector.Parse(postMovePath); err == nil && sel.Scheme == selector.Series {
		postMovePath = sel.Relative
	}
	out := trashfile.Record{
		Path:       postMovePath,
		Source:     rec.Source,
		Resolution: rec.Resolution,
		Codec:      rec.Codec,
		Size:       rec.Size,
		MTime:      rec.MTime,
		Companions: make([]trashfile.Companion, 0, len(rec.Companions)),
		Attrs:      media.CloneAttrs(rec.Attrs),
	}
	for _, c := range rec.Companions {
		out.Companions = append(out.Companions, trashfile.Companion{
			Path:     c.Path,
			Role:     c.Role,
			Language: c.Language,
			Label:    c.Label,
			Size:     c.Size,
			MTime:    c.MTime,
		})
	}
	return out
}

// runSteps iterates the plan's steps in order, dispatches by Kind, and
// appends per-step events. Returns (applied count, terminal error).
// runSteps executes the plan in order. On success returns the IDs of
// every applied step + nil. On failure returns the IDs applied before
// the failure, the failed step, and an *ApplyStepError wrapping the
// underlying primitive error. Log append failures short-circuit with
// the log error (no ApplyStepError wrap — the failure isn't on a
// step's primitive but on the forensic record).
func (e *executor) runSteps(ctx context.Context) ([]string, *Step, error) {
	total := len(e.in.Plan.Steps)
	progress.Start(ctx, "reconcile_apply", fmt.Sprintf("Applying %d step(s)", total), total)
	applied := make([]string, 0, total)
	for i := range e.in.Plan.Steps {
		if err := ctx.Err(); err != nil {
			return applied, nil, err
		}
		step := e.in.Plan.Steps[i]
		progress.Update(ctx, "reconcile_apply", stepSummary(step), i+1, total)
		stepErr := e.executeStep(step)
		if logErr := e.in.Log.AppendEvent(e.deps.Now(), step.ID, stepErr); logErr != nil {
			return applied, nil, logErr
		}
		if stepErr != nil {
			progress.Failure(ctx, "reconcile_apply",
				fmt.Sprintf("Step %d/%d failed: %s", i+1, total, stepSummary(step)),
				i+1, total)
			return applied, &step, &ApplyStepError{
				StepID:    step.ID,
				StepKind:  step.Kind,
				OwnerKind: step.Owner.Kind,
				From:      step.From,
				To:        step.To,
				Path:      step.Path,
				Err:       stepErr,
			}
		}
		applied = append(applied, step.ID)
		if e.log != nil {
			attrs := []any{
				"stepID", step.ID,
				"kind", string(step.Kind),
				"owner", string(step.Owner.Kind),
			}
			if step.Path != "" {
				attrs = append(attrs, "path", step.Path)
			}
			if step.From != "" {
				attrs = append(attrs, "from", step.From)
			}
			if step.To != "" {
				attrs = append(attrs, "to", step.To)
			}
			e.log.Info("apply step done", attrs...)
		}
	}
	progress.Success(ctx, "reconcile_apply", fmt.Sprintf("Applied %d step(s)", len(applied)), total)
	return applied, nil, nil
}

func stepSummary(step Step) string {
	switch step.Kind {
	case StepFileMove:
		return fmt.Sprintf("move %s", filepath.Base(step.From))
	case StepDirRemove:
		return fmt.Sprintf("remove %s", filepath.Base(step.Path))
	default:
		return string(step.Kind)
	}
}

// executeStep dispatches one step to its primitive. file_move →
// SafeMoveFile + post-move ancestor prune; dir_remove → removeIfEmpty.
func (e *executor) executeStep(step Step) error {
	switch step.Kind {
	case StepFileMove:
		from := e.absolutize(step.From)
		if err := fsop.SafeMoveFile(from, e.absolutize(step.To)); err != nil {
			return err
		}
		// Auto-prune: walk up from the source dir, removing each
		// truly-empty parent until we hit a non-empty dir, the
		// series root, the library root, or the OS root. Best
		// effort — failures are logged + swallowed; the move
		// succeeded and a leftover empty dir is cosmetic.
		e.pruneEmptyAncestors(filepath.Dir(from))
		return nil
	case StepDirRemove:
		_, err := removeDirIfEmpty(e.absolutize(step.Path))
		return err
	default:
		return fmt.Errorf("reconcile: unsupported step kind %q", step.Kind)
	}
}

// pruneEmptyAncestors walks parents starting from start, removing each
// truly-empty directory. Stops on first non-empty parent, on the series
// root, library root, inbox root, filesystem root, or on a removal error.
// Paths outside all three roots are never touched. Per-iteration failures
// are logged at debug + treated as "stop here"; they never abort the
// surrounding step.
func (e *executor) pruneEmptyAncestors(start string) {
	if start == "" {
		return
	}
	seriesRoot := filepath.Clean(e.seriesDir)
	libRoot := filepath.Clean(e.deps.LibRoot)
	inboxRoot := ""
	if e.deps.InboxRoot != "" {
		inboxRoot = filepath.Clean(e.deps.InboxRoot)
	}
	current := filepath.Clean(start)
	for {
		if isPruneBoundary(current, seriesRoot, libRoot, inboxRoot) {
			return
		}
		if !isInsidePruneRoot(current, seriesRoot, libRoot, inboxRoot) {
			return
		}
		removed, err := removeDirIfEmpty(current)
		if err != nil {
			if e.log != nil {
				e.log.Debug("auto-prune: stop on error", "dir", current, "err", err)
			}
			return
		}
		if !removed {
			// Either non-empty or already gone — either way the
			// walk should stop. removeDirIfEmpty returns
			// removed=false for both "non-empty" and "missing"
			// cases; we choose to stop on missing too because if
			// the source's immediate parent is already gone, an
			// upstream sibling op already pruned the chain.
			return
		}
		if e.log != nil {
			e.log.Debug("auto-prune removed empty dir", "dir", current)
		}
		current = filepath.Dir(current)
	}
}

// isPruneBoundary reports whether current sits at a directory the
// auto-prune walk must never remove: a series root, the library root,
// the inbox root (when configured), or the filesystem root. Returning
// true tells the caller to stop walking.
func isPruneBoundary(current, seriesRoot, libRoot, inboxRoot string) bool {
	if current == seriesRoot || current == libRoot {
		return true
	}
	if filepath.Dir(current) == current {
		return true
	}
	if inboxRoot != "" && current == inboxRoot {
		return true
	}
	return false
}

// isInsidePruneRoot reports whether current lives under any of the
// three known roots. Used as a safety gate before removing a
// directory — prevents climbing out of the library/inbox into
// arbitrary host directories.
func isInsidePruneRoot(current, seriesRoot, libRoot, inboxRoot string) bool {
	if isDescendantOf(current, seriesRoot) {
		return true
	}
	if isDescendantOf(current, libRoot) {
		return true
	}
	if inboxRoot != "" && isDescendantOf(current, inboxRoot) {
		return true
	}
	return false
}

// absolutize resolves a step path to an absolute filesystem path.
//
//   - "inbox:..." → resolved via deps.InboxRoot
//   - "series:..." → resolved via e.seriesDir
//   - absolute path → passed through
//   - bare relative → joined against e.seriesDir (legacy fallback)
//
// Bare-relative is kept as a fallback so older plan jsonl files (pre-
// series: refactor) still apply during a rolling deploy. New plans
// emit explicit series: prefix.
func (e *executor) absolutize(p string) string {
	if sel, err := selector.Parse(p); err == nil {
		switch sel.Scheme {
		case selector.Inbox:
			if e.deps.InboxRoot != "" {
				return sel.Resolve(e.deps.InboxRoot)
			}
		case selector.Series:
			return sel.Resolve(e.seriesDir)
		}
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(e.seriesDir, filepath.FromSlash(p))
}

// isDescendantOf reports whether path is a descendant of root (or equal to
// root). Both paths must be Clean already.
func isDescendantOf(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}

// removeDirIfEmpty refuses if the directory contains any entries
// (including hidden files like .DS_Store). Returns (true, nil) when
// the directory was removed; (false, nil) when the directory is
// non-empty (TOCTOU defense — operator handles cleanup) or already
// absent. Returns (false, err) on any other error.
//
// The two (false, nil) cases are intentionally not distinguished:
// every current caller (StepDirRemove, pruneEmptyAncestors) treats
// "non-empty" and "already gone" identically — both mean "stop
// trying to remove this dir." If a future caller needs to tell them
// apart, switch to a sentinel error or three-value return.
func removeDirIfEmpty(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if len(entries) > 0 {
		return false, nil // skip silently
	}
	if err := os.Remove(path); err != nil {
		return false, err
	}
	return true, nil
}

// applyPostStateMutations walks the plan's steps and mutates the
// loaded series in place to reflect successful execution: episode
// staged → active promotion, stagedTrash drain, stagedExtras drain.
//
// Per-owner aggregation: a step's owner attributes it to a higher-
// level intent. We treat per-owner success atomically — if every step
// owned by an episode/trash/extra completed (apply only reaches this
// path on full success), the owner's effect lands on the model.
func (e *executor) applyPostStateMutations(model *domainseries.Series) error {
	episodeRefs, standaloneTrashIDs, extraIDs := collectPlanOwnerIDs(e.in.Plan)
	if err := e.updateEpisodeRecordsAfterApply(model, episodeRefs); err != nil {
		return err
	}
	removeStandaloneTrashByIDs(model, standaloneTrashIDs)
	removeStandaloneExtrasByIDs(model, extraIDs)
	return nil
}

// collectPlanOwnerIDs walks plan.Steps once and partitions the touched
// owners into per-axis sets. Replaced-active episodes (OwnerTrash with
// non-zero OriginalEpisode) are intentionally not tracked here — the
// paired OwnerEpisode step's PromoteStaged will replace the Active
// record in updateEpisodeRecordsAfterApply, so no extra bookkeeping
// is needed for them.
func collectPlanOwnerIDs(plan Plan) (
	episodeRefs map[refs.Episode]struct{},
	standaloneTrashIDs map[string]struct{},
	extraIDs map[string]struct{},
) {
	episodeRefs = map[refs.Episode]struct{}{}
	standaloneTrashIDs = map[string]struct{}{}
	extraIDs = map[string]struct{}{}
	for _, step := range plan.Steps {
		switch step.Owner.Kind {
		case OwnerEpisode:
			episodeRefs[step.Owner.EpisodeRef] = struct{}{}
		case OwnerTrash:
			if step.Owner.OriginalEpisode.IsZero() {
				standaloneTrashIDs[step.Owner.TrashID] = struct{}{}
			}
		case OwnerExtra:
			extraIDs[step.Owner.ExtraID] = struct{}{}
		}
	}
	return episodeRefs, standaloneTrashIDs, extraIDs
}

// updateEpisodeRecordsAfterApply rewrites active/staged record paths
// to their post-apply canonical destinations and promotes staged →
// active for episodes that had a staged record.
//
// Three cases per touched episode:
//   - Staged-add / staged-replace: rewrite Staged paths to the
//     canonical To, then PromoteStaged.
//   - Active-only canonical move (intent="move"): no Staged record;
//     rewrite Active paths in place. Without this the active.Path
//     stored in series.json lags the filesystem until the next scan
//     re-derives.
//   - Episode missing from the model: skip (already pruned).
func (e *executor) updateEpisodeRecordsAfterApply(
	model *domainseries.Series,
	episodeRefs map[refs.Episode]struct{},
) error {
	for ep := range episodeRefs {
		episode, ok := model.Episodes[ep]
		if !ok {
			continue
		}
		primary, companions := e.episodePathsAfter(ep)
		switch {
		case episode.Staged != nil:
			rewriteRecordPaths(episode.Staged, primary, companions)
			// Refresh Size/MTime from the post-move file so the next
			// scan's (size,mtime) fingerprint check matches and skips
			// re-probing. Stage captured mtime at stage time; cross-FS
			// copy + Chtimes propagates the file's then-current mtime,
			// which can differ if the source was touched in the gap.
			e.refreshRecordFacts(episode.Staged)
			model.Episodes[ep] = episode
			if _, err := model.PromoteStaged(ep); err != nil {
				return fmt.Errorf("reconcile: promote staged for %s: %w", ep, err)
			}
		case episode.Active != nil && primary != "":
			rewriteRecordPaths(episode.Active, primary, companions)
			e.refreshRecordFacts(episode.Active)
			model.Episodes[ep] = episode
		}
	}
	return nil
}

// rewriteRecordPaths assigns rec.Path = primary and overlays the
// per-companion path slice in place. Companions beyond len(paths)
// keep their original Path — defensive against length mismatches
// from upstream plan generation.
func rewriteRecordPaths(rec *media.Record, primary string, paths []string) {
	rec.Path = primary
	for i := range rec.Companions {
		if i < len(paths) {
			rec.Companions[i].Path = paths[i]
		}
	}
}

// removeStandaloneTrashByIDs drops every stagedTrash entry whose id
// is in the set. Invalid ULIDs are skipped silently — the upstream
// plan-builder already enforced the format.
func removeStandaloneTrashByIDs(model *domainseries.Series, ids map[string]struct{}) {
	for id := range ids {
		uid, err := ulid.Parse(id)
		if err != nil {
			continue
		}
		model.RemoveStagedTrash(uid)
	}
}

// removeStandaloneExtrasByIDs drops every stagedExtras entry whose id
// is in the set.
func removeStandaloneExtrasByIDs(model *domainseries.Series, ids map[string]struct{}) {
	for id := range ids {
		uid, err := ulid.Parse(id)
		if err != nil {
			continue
		}
		model.RemoveStagedExtra(uid)
	}
}

// refreshRecordFacts re-stats the file at record.Path and its
// companions, overwriting Size + MTime in place. Best-effort: a stat
// failure (file moved off-disk before scan reached this point) leaves
// the prior values untouched and logs a debug line.  Truncation to
// second matches scan.statFacts so the (size,mtime) fingerprint
// comparison there sees byte-equal values.
func (e *executor) refreshRecordFacts(record *media.Record) {
	if record == nil {
		return
	}
	abs := e.absolutize(record.Path)
	if info, err := os.Stat(abs); err == nil {
		record.Size = info.Size()
		record.MTime = info.ModTime().UTC().Truncate(time.Second)
	} else if e.log != nil {
		e.log.Debug("apply post-state stat failed", "path", abs, "err", err)
	}
	for i := range record.Companions {
		cAbs := e.absolutize(record.Companions[i].Path)
		info, err := os.Stat(cAbs)
		if err != nil {
			if e.log != nil {
				e.log.Debug("apply post-state companion stat failed", "path", cAbs, "err", err)
			}
			continue
		}
		record.Companions[i].Size = info.Size()
		record.Companions[i].MTime = info.ModTime().UTC().Truncate(time.Second)
	}
}

// episodePathsAfter returns (primary path, []companion paths) for the
// given episode based on the plan's episode-owner steps' To fields.
// Plan ordering puts the primary stage move first, then companions.
func (e *executor) episodePathsAfter(ep refs.Episode) (primaryPath string, companionPaths []string) {
	var primary string
	var companions []string
	for _, step := range e.in.Plan.Steps {
		if step.Owner.Kind != OwnerEpisode || step.Owner.EpisodeRef != ep {
			continue
		}
		if step.Kind != StepFileMove {
			continue
		}
		if primary == "" {
			primary = step.To
		} else {
			companions = append(companions, step.To)
		}
	}
	return primary, companions
}

// acquireClaim sets in_progress on the series and CAS-writes. Surfaces
// InProgressError for live same-token holders, BusyError for live
// cross-token / cross-host holders, and silently breaks stale same-host
// claims.
func acquireClaim(deps Deps, loaded *domainseries.Series, token string) (coord.Holder, error) {
	if existing := loaded.InProgress; existing != nil {
		if !coord.IsStaleHolder(*existing) {
			if existing.Op == "reconcile_apply" && existing.Token == token {
				return coord.Holder{}, &InProgressError{Token: token, Holder: *existing}
			}
			return coord.Holder{}, &coord.BusyError{Scope: coord.SeriesScope(loaded.Ref), Holder: *existing}
		}
		// Stale; fall through to overwrite.
	}
	holder := coord.NewHolder("reconcile_apply", token)
	loaded.InProgress = &holder
	if err := seriesfile.SaveCAS(deps.LibRoot, loaded, coord.NewMutator("reconcile_apply_claim")); err != nil {
		return coord.Holder{}, err
	}
	return holder, nil
}

func releaseClaim(deps Deps, ref refs.Series, holder coord.Holder, expectedHash string) error {
	loaded, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		return err
	}
	if !claimMatches(loaded.InProgress, holder) {
		return nil
	}
	loaded.InProgress = nil
	loaded.Hash = expectedHash
	return seriesfile.SaveCAS(deps.LibRoot, loaded, coord.NewMutator("reconcile_apply_release"))
}

func claimMatches(found *coord.Holder, want coord.Holder) bool {
	if found == nil {
		return false
	}
	return found.PID == want.PID && found.Host == want.Host && found.Started.Equal(want.Started) && found.Token == want.Token
}

// validateAppliedSnapshot re-reads series.json bytes and confirms they
// hash to the snapshot recorded in the plan header. Detects drift
// between plan and apply; caller re-plans.
func validateAppliedSnapshot(root string, ref refs.Series, plan Plan) error {
	data, err := os.ReadFile(paths.SeriesMetadata(root, ref))
	if err != nil {
		return err
	}
	if Snapshot(data) != plan.Header.Snapshot {
		return StaleSnapshotError{Series: ref}
	}
	return nil
}
