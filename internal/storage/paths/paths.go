// Package paths owns canonical path construction for every Kura-managed
// directory and file. Other packages call into paths/ instead of building
// layout strings themselves; only this package knows that series.json lives
// at <library>/<series>/.kura/series.json, that the library index lives at
// <library>/.kura/index.jsonl, and so on.
//
// Generic absolute+relative joins (e.g. resolving a user-supplied relative
// path inside a known absolute series directory) are NOT in scope and stay
// inline at the call site -- those operations are not Kura layout facts.
package paths

import (
	"fmt"
	"path/filepath"

	"github.com/wyvernzora/kura/internal/domain/refs"
)

// On-disk layout constants. Exported so callers that need to compare or
// log them have one source of truth.
const (
	KuraDir             = ".kura"
	SeriesFileName      = "series.json"
	IndexFileName       = "index.jsonl"
	LegacyIndexFileName = "index.tsv"
	TrashDirName        = "trash"
	TrashMetaName       = "meta.json"
	PlanDirName         = "reconcile"
	PlanExtension       = ".jsonl"
	ExtraDirName        = "Extra"
)

// LibraryKuraDir returns <libRoot>/.kura/.
func LibraryKuraDir(libRoot string) string {
	return filepath.Join(libRoot, KuraDir)
}

// IndexFile returns <libRoot>/.kura/index.jsonl.
func IndexFile(libRoot string) string {
	return filepath.Join(LibraryKuraDir(libRoot), IndexFileName)
}

// LegacyIndexFile returns <libRoot>/.kura/index.tsv. The TSV format predates
// Index v2 and is no longer read or written; bootstrap removes it after the
// first JSONL save so a single command run sweeps the artifact.
func LegacyIndexFile(libRoot string) string {
	return filepath.Join(LibraryKuraDir(libRoot), LegacyIndexFileName)
}

// SeriesDir returns <libRoot>/<ref>/.
func SeriesDir(libRoot string, ref refs.Series) string {
	return filepath.Join(libRoot, filepath.FromSlash(ref.String()))
}

// SeriesKuraDir returns <libRoot>/<ref>/.kura/.
func SeriesKuraDir(libRoot string, ref refs.Series) string {
	return filepath.Join(SeriesDir(libRoot, ref), KuraDir)
}

// SeriesMetadata returns <libRoot>/<ref>/.kura/series.json.
func SeriesMetadata(libRoot string, ref refs.Series) string {
	return filepath.Join(SeriesKuraDir(libRoot, ref), SeriesFileName)
}

// TrashDir returns <libRoot>/<ref>/.kura/trash/.
func TrashDir(libRoot string, ref refs.Series) string {
	return filepath.Join(SeriesKuraDir(libRoot, ref), TrashDirName)
}

// TrashEntry returns <libRoot>/<ref>/.kura/trash/<ulid>/.
func TrashEntry(libRoot string, ref refs.Series, ulid string) string {
	return filepath.Join(TrashDir(libRoot, ref), ulid)
}

// TrashMeta returns <libRoot>/<ref>/.kura/trash/<ulid>/meta.json.
func TrashMeta(libRoot string, ref refs.Series, ulid string) string {
	return filepath.Join(TrashEntry(libRoot, ref, ulid), TrashMetaName)
}

// TrashMedia returns <libRoot>/<ref>/.kura/trash/<ulid>/<basename>.
func TrashMedia(libRoot string, ref refs.Series, ulid, basename string) string {
	return filepath.Join(TrashEntry(libRoot, ref, ulid), basename)
}

// TrashRel returns the wire-shape relative path .kura/trash/<ulid>/<basename>
// (forward slashes), used inside reconcile FileMove records that target the
// trash directory.
func TrashRel(ulid, basename string) string {
	return filepath.ToSlash(filepath.Join(KuraDir, TrashDirName, ulid, basename))
}

// PlanDir returns <libRoot>/<ref>/.kura/reconcile/.
func PlanDir(libRoot string, ref refs.Series) string {
	return filepath.Join(SeriesKuraDir(libRoot, ref), PlanDirName)
}

// PlanFile returns <libRoot>/<ref>/.kura/reconcile/<token>.jsonl.
func PlanFile(libRoot string, ref refs.Series, token string) string {
	return filepath.Join(PlanDir(libRoot, ref), token+PlanExtension)
}

// SeasonDir returns <libRoot>/<ref>/Season <N>/. Season 0 maps to the series
// root: specials live next to the series, not under a Season 0 directory.
func SeasonDir(libRoot string, ref refs.Series, season int) string {
	if season == 0 {
		return SeriesDir(libRoot, ref)
	}
	return filepath.Join(SeriesDir(libRoot, ref), fmt.Sprintf("Season %d", season))
}

// SeasonExtraDir returns <libRoot>/<ref>/Season <N>/Extra/.
func SeasonExtraDir(libRoot string, ref refs.Series, season int) string {
	return filepath.Join(SeasonDir(libRoot, ref, season), ExtraDirName)
}

// SeasonExtraRel returns the wire-shape relative path "Season <N>/Extra/"
// (forward slashes). Season 0 returns "Extra/" — specials extras live
// directly under the series root. Used for prefix-collision pre-checks.
func SeasonExtraRel(season int) string {
	if season == 0 {
		return ExtraDirName + "/"
	}
	return fmt.Sprintf("Season %d/%s/", season, ExtraDirName)
}

// ExtraRel returns the wire-shape relative path
// "Season <N>/Extra/[<prefix>/]<basename>" (forward slashes), used inside
// reconcile FileMove records that target the extras directory. prefix may
// be empty (no sub-folder). Caller is responsible for sanitizing prefix
// (no path separators or "..") before calling. Season 0 places extras
// directly under "Extra/" (specials extras at the series root).
func ExtraRel(season int, prefix, basename string) string {
	out := SeasonExtraRel(season)
	if prefix != "" {
		out += prefix + "/"
	}
	out += basename
	return out
}

// EpisodeMedia returns the absolute path to an episode media file at
// <libRoot>/<ref>/Season <N>/<filename>.
func EpisodeMedia(libRoot string, ref refs.Series, season int, filename string) string {
	return filepath.Join(SeasonDir(libRoot, ref, season), filename)
}

// EpisodeMediaRel returns the wire-shape relative path "Season <N>/<filename>"
// (forward slashes), used inside series.json active records and reconcile
// FileMove records for episode media. Season 0 returns just the filename.
func EpisodeMediaRel(season int, filename string) string {
	if season == 0 {
		return filename
	}
	return fmt.Sprintf("Season %d/%s", season, filename)
}
