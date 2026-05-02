package reconcile

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/series/wire"
	"github.com/wyvernzora/kura/internal/trash"
)

func (h Runner) writeTrash(episode refs.Episode, record MediaRecord, replaced Replaced) error {
	id, err := trashIDFromPath(replaced.To)
	if err != nil {
		return err
	}
	record.Path = replaced.To
	for index := range record.Companions {
		if index < len(replaced.Companions) {
			record.Companions[index].Path = replaced.Companions[index].To
		}
	}
	return trash.Write(h.root(), h.ref, trash.Meta{
		ID:        id,
		Episode:   episode,
		TrashedAt: h.now().UTC(),
		Record:    trashRecord(record),
	})
}

func trashRecord(in MediaRecord) trash.Record {
	out := trash.Record{
		Path:       in.Path,
		Source:     in.Source,
		Resolution: in.Resolution,
		Codec:      in.Codec,
		Size:       in.Size,
		MTime:      in.MTime,
		Companions: make([]trash.Companion, 0, len(in.Companions)),
	}
	for _, companion := range in.Companions {
		out.Companions = append(out.Companions, trash.Companion{
			Path:     companion.Path,
			Role:     companion.Role,
			Language: companion.Language,
			Label:    companion.Label,
			Size:     companion.Size,
			MTime:    companion.MTime,
		})
	}
	return out
}

func trashCompanionMoves(id ulid.ULID, companions []CompanionRecord) []FileMove {
	moves := make([]FileMove, 0, len(companions))
	for _, companion := range companions {
		moves = append(moves, FileMove{From: companion.Path, To: trashRelPath(id, companion.Path)})
	}
	return moves
}

func trashRelPath(id ulid.ULID, path string) string {
	return filepath.ToSlash(filepath.Join(wire.KuraDir, trash.DirName, id.String(), filepath.Base(path)))
}

func trashIDFromPath(path string) (ulid.ULID, error) {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) < 3 {
		return ulid.ULID{}, fmt.Errorf("series: trash path %q missing ulid", path)
	}
	return ulid.Parse(parts[2])
}
