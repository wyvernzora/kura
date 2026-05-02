package reconcile

import (
	"time"

	"github.com/wyvernzora/kura/internal/domain/refs"
)

// Plan is the immutable intent of a reconcile under the v2 plan
// format: header metadata plus an ordered, fully-unrolled step
// sequence. Apply iterates steps in order, logging an Event per
// attempt and a final Result.
//
// Schema: header line + N step lines + (when applied) M event lines + 1
// result line, all JSONL with a `type:` discriminator. Apply phase
// becomes "for each step, execute, log event"; planning is where the
// intelligence lives.
//
// Plan lives alongside the legacy Plan (in plan.go) until the
// migration replaces it. New code targets Plan.
type Plan struct {
	Header Header
	Steps  []Step
}

// HasWork reports whether the plan contains any steps.
func (p Plan) HasWork() bool {
	return len(p.Steps) > 0
}

// Header is the per-plan envelope: token, lifetime, series identity,
// snapshot of series.json bytes at plan time.
type Header struct {
	SchemaVersion int
	Token         string
	CreatedAt     time.Time
	ExpiresAt     time.Time
	Series        refs.Series
	Snapshot      string
}

// StepKind discriminates Step variants. Closed at v2.
type StepKind string

const (
	// StepFileMove moves one file from From → To. Both paths are
	// series-root-relative slash form unless From is absolute (extras
	// source files can live outside the series root).
	StepFileMove StepKind = "file_move"

	// StepDirRemove removes one directory if empty. Path is
	// series-root-relative slash form. Apply refuses to remove a
	// non-empty directory (including hidden files); skips silently
	// in that case to defend against TOCTOU.
	StepDirRemove StepKind = "dir_remove"
)

// Step is one indivisible file-system mutation in a plan. ID is a
// deterministic 16-char base32 hash derived at plan time so the same
// series state always produces the same step IDs (see DeriveStepID).
//
// File-move steps populate From + To. Dir-remove steps populate Path
// (From and To are empty). Owner attributes the step to the
// higher-level intent that produced it (an episode, a trash item, an
// extras placement) so failure surfacing and recovery tooling can
// correlate.
type Step struct {
	ID    string
	Kind  StepKind
	Owner Owner
	From  string
	To    string
	Path  string
}

// OwnerKind discriminates Owner variants. Closed at v2.
type OwnerKind string

const (
	// OwnerEpisode: the step is part of an episode stage's canonical
	// move (primary file, companion files).
	OwnerEpisode OwnerKind = "episode"

	// OwnerTrash: the step moves a file to .kura/trash/<id>/. Two
	// sub-cases:
	//   - Replaced active: OriginalEpisode populated; Record carries
	//     the displaced active record's full facts (source/resolution/
	//     codec from the recorded media).
	//   - Standalone stagedTrash: OriginalEpisode empty; Record carries
	//     the original series-relative path + size + mtime + companions
	//     (no source/resolution).
	// In both cases Record is populated so apply's setup phase has
	// everything it needs to write trashfile.Meta (preserves the file's
	// origin so `kura trash restore` can recover it).
	OwnerTrash OwnerKind = "trash"

	// OwnerExtra: the step moves a file or removes a directory under
	// Season N/Extra/[Prefix]/ for a stagedExtras placement.
	OwnerExtra OwnerKind = "extra"
)

// Owner attributes a Step to the high-level intent (episode / trash /
// extra) that produced it. ID/StableKey identifies the specific intent
// (episode ref, trash ULID, extra ULID); OwnerKind-specific fields
// carry the data apply needs that isn't already in the Step itself.
type Owner struct {
	Kind OwnerKind

	// EpisodeRef populated when Kind == OwnerEpisode.
	EpisodeRef refs.Episode

	// EpisodeIntent populated when Kind == OwnerEpisode. Captures the
	// high-level intent ("add" / "move" / "replace") so the response
	// projection can surface it. Pure metadata; does not affect step
	// execution.
	EpisodeIntent string

	// TrashID populated when Kind == OwnerTrash. ULID string;
	// addresses the .kura/trash/<id>/ bucket.
	TrashID string

	// OriginalEpisode populated when Kind == OwnerTrash AND the trash
	// step represents an active record being displaced by an episode
	// stage. Empty for standalone stagedTrash items.
	OriginalEpisode refs.Episode

	// Record populated when Kind == OwnerTrash. Carries the data
	// needed by apply's setup phase to write trashfile.Meta.
	Record *ReplacedRecord

	// StagedRecord populated when Kind == OwnerEpisode. Carries the
	// incoming file's media facts (source / resolution / codec / size /
	// mtime / companions) so planToResponse can surface them without
	// parsing canonical filenames. For "add" / "replace" intents this
	// reflects the staged record; for "move" intents it reflects the
	// active record being canonicalized.
	StagedRecord *ReplacedRecord

	// ExtraID populated when Kind == OwnerExtra.
	ExtraID string

	// Season populated when Kind == OwnerExtra.
	Season int

	// Prefix populated when Kind == OwnerExtra (may be empty).
	Prefix string
}

// ReplacedRecord carries the per-trashed-file facts apply needs to
// write a self-describing trashfile.Meta entry before the move runs.
// Populated for both replaced-active records (full media facts) and
// standalone stagedTrash items (path + size + mtime + companions).
//
// Source / Resolution / Codec are populated for replaced-active only;
// standalone stagedTrash leaves them empty (no mediainfo probe at
// stage time).
type ReplacedRecord struct {
	Path       string
	Source     string
	Resolution string
	Codec      string
	Size       int64
	MTime      time.Time
	Companions []ReplacedCompanion
}

// ReplacedCompanion mirrors the per-companion fields trashfile.Meta
// records.
type ReplacedCompanion struct {
	Path     string
	Role     string
	Language string
	Label    string
	Size     int64
	MTime    time.Time
}

// Event is one terminal record per attempted Step in the apply log.
// Step references the StepID that produced this attempt; Error is
// populated on failure.
type Event struct {
	At    time.Time
	Step  string
	Error string
}

// Result is the terminal apply-log line: success / failure with the
// applied step count.
type Result struct {
	At           time.Time
	Status       string
	AppliedSteps int
	Error        string
}
