package reconcile

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/domain/filename"
	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/domain/selector"
	"github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/textnorm"
)

// libraryStep wraps a series-relative path into a series: selector for
// step.From / step.To. Pass-through for already-prefixed strings
// (inbox:/series:) and for absolute paths (legacy persisted records or
// extras whose source remains absolute post-resolve).
func libraryStep(rel string) string {
	if rel == "" {
		return rel
	}
	if strings.HasPrefix(rel, string(selector.Inbox)+":") || strings.HasPrefix(rel, string(selector.Series)+":") {
		return rel
	}
	if filepath.IsAbs(rel) {
		return rel
	}
	return string(selector.Series) + ":" + rel
}

// planEpisodes builds the ordered step set for every spine slot's
// episode-level transition: staged-add, staged-replace (with active
// being trashed), and active-only canonical moves.
//
// Step ordering per episode (replacing-active case):
//
//  1. Trash-active file_move (primary) → moves displaced active media
//     into .kura/trash/<id>/<basename>. Owner = trash with
//     OriginalEpisode set and Record populated from the active record.
//  2. Trash-active companion file_moves.
//  3. Primary stage file_move → moves staged media into canonical
//     Season N/<basename>. Owner = episode.
//  4. Stage companion file_moves.
//
// Across episodes, episodes are sorted by ref so the step set is
// deterministic.
func planEpisodes(token string, seriesRef refs.Series, seriesDir string, model *series.Series) ([]Step, error) {
	episodeRefs := make([]refs.Episode, 0, len(model.Episodes))
	for ref := range model.Episodes {
		episodeRefs = append(episodeRefs, ref)
	}
	sort.Slice(episodeRefs, func(i, j int) bool {
		return episodeRefs[i].String() < episodeRefs[j].String()
	})

	out := make([]Step, 0)
	for _, episodeRef := range episodeRefs {
		ep := model.Episodes[episodeRef]
		switch {
		case ep.Staged != nil:
			steps, err := planStagedEpisode(token, seriesRef, seriesDir, episodeRef, ep)
			if err != nil {
				return nil, err
			}
			out = append(out, steps...)
		case ep.Active != nil:
			steps, err := planActiveEpisode(token, seriesRef, seriesDir, episodeRef, *ep.Active)
			if err != nil {
				return nil, err
			}
			out = append(out, steps...)
		}
	}
	return out, nil
}

// planStagedEpisode covers the staged-add and staged-replace cases for
// one episode slot. Replaced-active steps emit ahead of the primary
// stage move so apply trashes the existing file before installing the
// new one.
func planStagedEpisode(token string, seriesRef refs.Series, seriesDir string, episodeRef refs.Episode, ep series.Episode) ([]Step, error) {
	target := canonicalEpisodePath(seriesRef, episodeRef, *ep.Staged)

	var steps []Step

	intent := "add"
	if ep.Active != nil && !mediaPathsEquivalent(seriesDir, *ep.Active, *ep.Staged) {
		// Replace: trash the current active file (and companions)
		// before the primary move drops the new file in place.
		intent = "replace"
		activeRel, err := relativeRecord(seriesDir, *ep.Active)
		if err != nil {
			return nil, err
		}
		id := ulid.Make()
		owner := Owner{
			Kind:            OwnerTrash,
			TrashID:         id.String(),
			OriginalEpisode: episodeRef,
			Record:          recordFromMedia(activeRel),
		}
		trashTo := paths.TrashRel(id.String(), filepath.Base(activeRel.Path))
		steps = append(steps, makeFileMove(token, owner, libraryStep(activeRel.Path), trashTo))
		for _, c := range activeRel.Companions {
			cTo := paths.TrashRel(id.String(), filepath.Base(c.Path))
			steps = append(steps, makeFileMove(token, owner, libraryStep(c.Path), cTo))
		}
	}

	// Primary stage move. Staged.Path already carries inbox: scheme;
	// libraryStep is a no-op on it. step.To stays bare so post-apply
	// promotion can write Active.Path directly without selector
	// stripping.
	primaryOwner := Owner{
		Kind:          OwnerEpisode,
		EpisodeRef:    episodeRef,
		EpisodeIntent: intent,
		StagedRecord:  recordFromMedia(*ep.Staged),
	}
	steps = append(steps, makeFileMove(token, primaryOwner, libraryStep(ep.Staged.Path), target))

	// Companion moves (renamed alongside the new media basename).
	for _, m := range companionMoves(ep.Staged.Path, target, ep.Staged.Companions) {
		steps = append(steps, makeFileMove(token, primaryOwner, libraryStep(m.From), m.To))
	}

	return steps, nil
}

// planActiveEpisode covers a slot whose active media file is in the
// wrong canonical location and needs renaming. No trash; just move(s).
// Returns no steps when the file is already canonical AND companions
// don't need renaming.
//
// Path equivalence is NFC-normalized: NFC(a) == NFC(b) is treated as
// already-canonical. Some filesystems (SMB/AFP NAS, legacy HFS+) store
// basenames in NFD form regardless of the form last passed to rename;
// without NFC-equivalence the planner would emit a noop normalization
// move that the next scan immediately undoes, leaving the operator
// stuck in a loop.
func planActiveEpisode(token string, seriesRef refs.Series, seriesDir string, episodeRef refs.Episode, active media.Record) ([]Step, error) {
	rel, err := relativeRecord(seriesDir, active)
	if err != nil {
		return nil, err
	}
	target := canonicalEpisodePath(seriesRef, episodeRef, rel)
	companionMoves := companionMoves(rel.Path, target, rel.Companions)
	if pathsEquivalentNFC(target, rel.Path) && len(companionMoves) == 0 {
		return nil, nil
	}

	owner := Owner{
		Kind:          OwnerEpisode,
		EpisodeRef:    episodeRef,
		EpisodeIntent: "move",
		StagedRecord:  recordFromMedia(rel),
	}
	out := make([]Step, 0, 1+len(companionMoves))
	if !pathsEquivalentNFC(target, rel.Path) {
		out = append(out, makeFileMove(token, owner, libraryStep(rel.Path), target))
	}
	for _, m := range companionMoves {
		out = append(out, makeFileMove(token, owner, libraryStep(m.From), m.To))
	}
	return out, nil
}

// pathsEquivalentNFC reports whether two paths are equal after NFC
// normalization. Used to suppress no-op canonicalization moves on
// filesystems that return decomposed (NFD) basenames regardless of
// how the entry was created.
func pathsEquivalentNFC(a, b string) bool {
	if a == b {
		return true
	}
	return textnorm.NFC(a).String() == textnorm.NFC(b).String()
}

// mediaPathsEquivalent reports whether two media records refer to the
// same on-disk file once active-side absolutization and staged-side
// scheme prefixes are normalized away. Active.Path is absolute (post-
// Load); Staged.Path carries an inbox: or series: scheme. Used by
// the planner to detect in-place metadata-override stages where the
// staged record points at the existing active file — those skip the
// trash step and emit a noop / rename move only.
func mediaPathsEquivalent(seriesDir string, active, staged media.Record) bool {
	activeRel := active.Path
	if filepath.IsAbs(activeRel) {
		rel, err := filepath.Rel(seriesDir, activeRel)
		if err != nil {
			return false
		}
		activeRel = filepath.ToSlash(rel)
	}
	stagedRel := staged.Path
	switch {
	case strings.HasPrefix(stagedRel, string(selector.Inbox)+":"):
		return false
	case strings.HasPrefix(stagedRel, string(selector.Series)+":"):
		stagedRel = strings.TrimPrefix(stagedRel, string(selector.Series)+":")
	case filepath.IsAbs(stagedRel):
		rel, err := filepath.Rel(seriesDir, stagedRel)
		if err != nil {
			return false
		}
		stagedRel = filepath.ToSlash(rel)
	}
	return pathsEquivalentNFC(activeRel, stagedRel)
}

// canonicalEpisodePath composes the canonical "Season N/<basename>"
// relative path for the given episode + media facts.
func canonicalEpisodePath(seriesRef refs.Series, episodeRef refs.Episode, record media.Record) string {
	title := filename.CleanTitle(seriesRef.String())
	basename := filename.BuildMedia(title, episodeRef, filename.Facts{
		Source:     record.Source,
		Resolution: record.Resolution,
	}, filepath.Ext(record.Path)).String()
	return paths.EpisodeMediaRel(episodeRef.Season(), basename)
}

// relativeRecord returns a copy of an active record with paths rewritten
// relative to the series directory. seriesfile.Load absolutizes active
// paths in memory; the planner persists relative paths in step records.
func relativeRecord(seriesDir string, record media.Record) (media.Record, error) {
	out := media.CloneRecord(record)
	if filepath.IsAbs(out.Path) {
		rel, err := filepath.Rel(seriesDir, out.Path)
		if err != nil {
			return media.Record{}, err
		}
		out.Path = filepath.ToSlash(rel)
	}
	for i := range out.Companions {
		if filepath.IsAbs(out.Companions[i].Path) {
			rel, err := filepath.Rel(seriesDir, out.Companions[i].Path)
			if err != nil {
				return media.Record{}, err
			}
			out.Companions[i].Path = filepath.ToSlash(rel)
		}
	}
	return out, nil
}

// fileMove records a {From, To} pair before it gets wrapped into a
// Step. Internal-only.
type fileMove struct {
	From string
	To   string
}

// companionMoves computes the renamed destinations for companion files
// when the primary media file moves from oldMediaPath to newMediaPath.
// Companions whose target equals their current path are omitted.
func companionMoves(oldMediaPath, newMediaPath string, companions []media.Companion) []fileMove {
	oldBase := strings.TrimSuffix(filepath.Base(oldMediaPath), filepath.Ext(oldMediaPath))
	newBase := strings.TrimSuffix(filepath.Base(newMediaPath), filepath.Ext(newMediaPath))
	dir := filepath.Dir(newMediaPath)
	if dir == "." {
		dir = ""
	}
	out := make([]fileMove, 0, len(companions))
	for _, c := range companions {
		target := filepath.ToSlash(filepath.Join(dir, newBase+companionSuffix(filepath.Base(c.Path), oldBase)))
		if !pathsEquivalentNFC(target, c.Path) {
			out = append(out, fileMove{From: c.Path, To: target})
		}
	}
	return out
}

func companionSuffix(name, oldMediaBase string) string {
	if strings.HasPrefix(name, oldMediaBase+".") {
		return strings.TrimPrefix(name, oldMediaBase)
	}
	extension := compoundExtension(name)
	if extension == "" {
		return filepath.Ext(name)
	}
	return extension
}

func compoundExtension(name string) string {
	base := filepath.Base(name)
	index := strings.Index(base, ".")
	if index < 0 {
		return ""
	}
	return base[index:]
}

// recordFromMedia packs a media.Record's facts into a ReplacedRecord
// so an Owner can carry the file's full provenance (source / resolution
// / codec / size / mtime / companions). Used both for trash steps
// (displaced active record) and episode steps (incoming staged or
// canonicalized active record).
func recordFromMedia(rec media.Record) *ReplacedRecord {
	out := &ReplacedRecord{
		Path:       rec.Path,
		Source:     rec.Source.String(),
		Resolution: rec.Resolution.String(),
		Codec:      rec.Codec.String(),
		Size:       rec.Size,
		MTime:      rec.MTime,
		Companions: make([]ReplacedCompanion, 0, len(rec.Companions)),
	}
	for _, c := range rec.Companions {
		out.Companions = append(out.Companions, ReplacedCompanion{
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

// makeFileMove builds a file_move Step with a derived ID. owner / from /
// to / kind feed into DeriveStepID; the empty Path field reflects "this
// is a file move, not a dir remove."
func makeFileMove(token string, owner Owner, from, to string) Step {
	return Step{
		ID:    DeriveStepID(token, owner, StepFileMove, from, to, ""),
		Kind:  StepFileMove,
		Owner: owner,
		From:  from,
		To:    to,
	}
}

// makeDirRemove builds a dir_remove Step with a derived ID.
func makeDirRemove(token string, owner Owner, path string) Step {
	return Step{
		ID:    DeriveStepID(token, owner, StepDirRemove, "", "", path),
		Kind:  StepDirRemove,
		Owner: owner,
		Path:  path,
	}
}
