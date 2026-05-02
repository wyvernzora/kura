package reconcile

import (
	"path/filepath"
	"sort"

	"github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/storage/paths"
)

// planTrash builds the ordered step set for every standalone
// stagedTrash entry on the series. Each entry produces:
//
//  1. Primary file_move from the entry's series-relative path into
//     .kura/trash/<id>/<basename>.
//  2. Companion file_moves, one per recorded companion.
//
// Replaced-active steps for episode stages are NOT produced here —
// planEpisodes handles those because they're derived from the active
// record, not from the stagedTrash array. Owner.OriginalEpisode stays
// empty for standalone trash.
//
// Source / Resolution are intentionally NOT populated on the owner's
// Record; stage doesn't probe mediainfo for trash items. apply's setup
// phase still writes a trashfile.Meta entry so the file remains
// restorable via `kura trash restore` using the recorded original path.
//
// Output is sorted by stagedTrash entry ULID for determinism.
func planTrash(token string, seriesDir string, model *series.Series) ([]Step, error) {
	if len(model.StagedTrash) == 0 {
		return nil, nil
	}
	items := make([]series.StagedTrashItem, len(model.StagedTrash))
	copy(items, model.StagedTrash)
	sort.Slice(items, func(i, j int) bool { return items[i].ID.String() < items[j].ID.String() })

	out := make([]Step, 0, len(items))
	for _, item := range items {
		from, err := relativizeUnderSeries(seriesDir, item.Path)
		if err != nil {
			return nil, err
		}
		basename := filepath.Base(item.Path)
		to := paths.TrashRel(item.ID.String(), basename)
		owner := Owner{
			Kind:    OwnerTrash,
			TrashID: item.ID.String(),
			Record:  recordFromStagedTrash(from, item),
		}
		out = append(out, makeFileMove(token, owner, libraryStep(from), to))
		for _, c := range item.Companions {
			cFrom, err := relativizeUnderSeries(seriesDir, c.Path)
			if err != nil {
				return nil, err
			}
			cTo := paths.TrashRel(item.ID.String(), filepath.Base(c.Path))
			out = append(out, makeFileMove(token, owner, libraryStep(cFrom), cTo))
		}
	}
	return out, nil
}

// relativizeUnderSeries normalizes a stagedTrash path. stagedTrash
// invariants enforce the path is under the series root, so the
// resulting slash form contains no leading "..".
func relativizeUnderSeries(seriesDir, abs string) (string, error) {
	if !filepath.IsAbs(abs) {
		return filepath.ToSlash(abs), nil
	}
	rel, err := filepath.Rel(seriesDir, abs)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// recordFromStagedTrash packs the standalone stagedTrash entry's facts
// into a ReplacedRecord. Original path (series-relative) is preserved
// so `kura trash restore` can put the file back where it came from.
// Source / Resolution / Codec stay empty (no mediainfo probe at
// stage time).
func recordFromStagedTrash(originalPath string, item series.StagedTrashItem) *ReplacedRecord {
	out := &ReplacedRecord{
		Path:       originalPath,
		Size:       item.Size,
		MTime:      item.MTime,
		Companions: make([]ReplacedCompanion, 0, len(item.Companions)),
	}
	for _, c := range item.Companions {
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
