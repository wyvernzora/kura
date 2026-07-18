package seriesfile_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
	"github.com/wyvernzora/kura/internal/textnorm"
)

func TestSnapshotEncodeDecodeRoundTrip(t *testing.T) {
	libRoot, ref := setupFixtureLibrary(t)
	model, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	seriesDir := paths.SeriesDir(libRoot, ref)
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	model.CanonicalTitle = textnormFor(t, "Honzuki no Gekokujou")
	model.UserAliases = []textnorm.NFCString{textnormFor(t, "honzuki"), textnormFor(t, "bookworm")}
	model.Tags = []string{"Priority", "maintenance-requested", "PRIORITY"}
	model.InProgress = &coord.Holder{Op: "reconcile_apply", Token: "tok", PID: 44, Host: "host", Started: now}
	model.LastMutated = coord.Mutator{Op: "scan", PID: 45, Host: "host", At: now}
	model.StagedTrash = []series.StagedTrashItem{
		{
			ID:      ulid.MustParse("01ARYZ6S410000000000000000"),
			Path:    filepath.Join(seriesDir, "Season 1", "old.mkv"),
			Size:    10,
			MTime:   now,
			AddedAt: now,
			Companions: []media.Companion{
				{Path: filepath.Join(seriesDir, "Season 1", "old.en.srt"), Language: "en", Size: 2, MTime: now},
			},
		},
	}
	model.StagedExtras = []series.StagedExtraItem{
		{
			ID:      ulid.MustParse("01ARYZ6S410000000000000001"),
			Season:  1,
			Path:    "inbox:extras/booklet.pdf",
			Prefix:  "Booklets",
			AddedAt: now,
		},
	}

	data, err := seriesfile.Encode(libRoot, model)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if bytes.Contains(data, []byte("\n")) || bytes.Contains(data, []byte("  ")) {
		t.Fatalf("snapshot is not compact JSON: %q", data)
	}
	if !bytes.Contains(data, []byte(`"schemaVersion":3`)) {
		t.Fatalf("snapshot missing schemaVersion 3: %s", data)
	}
	if !bytes.Contains(data, []byte(`"dateAdded":"2026-04-20T03:00:00Z"`)) {
		t.Fatalf("snapshot missing dateAdded: %s", data)
	}
	if bytes.Contains(data, []byte(seriesDir)) {
		t.Fatalf("snapshot leaked absolute series dir: %s", data)
	}

	decoded, err := seriesfile.Decode(libRoot, ref, data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Ref != ref {
		t.Fatalf("Ref = %s, want %s", decoded.Ref, ref)
	}
	if decoded.Hash != "" {
		t.Fatalf("Hash = %q, want empty", decoded.Hash)
	}
	if !decoded.DateAdded.Equal(model.DateAdded) {
		t.Fatalf("DateAdded = %v, want %v", decoded.DateAdded, model.DateAdded)
	}
	ep1 := mustEpisode(t, "S01E0001")
	if got := decoded.Episodes[ep1].Active.Path; got != filepath.Join(seriesDir, "Season 1", "Bookworm - S01E01 (BDRip 1080p).mkv") {
		t.Fatalf("active path = %q", got)
	}
	if got := decoded.Episodes[ep1].Active.Companions[0].Path; got != filepath.Join(seriesDir, "Season 1", "Bookworm - S01E01 (BDRip 1080p).en.srt") {
		t.Fatalf("active companion path = %q", got)
	}
	ep2 := mustEpisode(t, "S01E0002")
	if got := decoded.Episodes[ep2].Staged.Path; got != "/inbox/Bookworm S01E02.mkv" {
		t.Fatalf("staged path = %q", got)
	}
	if got := decoded.StagedTrash[0].Path; got != filepath.Join(seriesDir, "Season 1", "old.mkv") {
		t.Fatalf("staged trash path = %q", got)
	}
	if got := decoded.StagedTrash[0].Companions[0].Path; got != filepath.Join(seriesDir, "Season 1", "old.en.srt") {
		t.Fatalf("staged trash companion path = %q", got)
	}
	if got := decoded.StagedExtras[0].Path; got != "inbox:extras/booklet.pdf" {
		t.Fatalf("staged extra path = %q", got)
	}
	if len(decoded.UserAliases) != 2 || decoded.UserAliases[0].String() != "honzuki" {
		t.Fatalf("UserAliases = %+v", decoded.UserAliases)
	}
	if len(decoded.Tags) != 2 || decoded.Tags[0] != "maintenance-requested" || decoded.Tags[1] != "priority" {
		t.Fatalf("Tags = %+v", decoded.Tags)
	}
	if decoded.InProgress == nil || decoded.InProgress.Op != "reconcile_apply" {
		t.Fatalf("InProgress = %+v", decoded.InProgress)
	}
	if decoded.LastMutated.Op != "scan" {
		t.Fatalf("LastMutated = %+v", decoded.LastMutated)
	}

	decoded.Episodes[ep1].Active.Path = "mutated"
	if model.Episodes[ep1].Active.Path == "mutated" {
		t.Fatal("decoded model aliases source model")
	}
}

func TestSnapshotDecodeRejectsWrongSchema(t *testing.T) {
	libRoot, ref := setupFixtureLibrary(t)
	model, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	data, err := seriesfile.Encode(libRoot, model)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	bad := []byte(strings.Replace(string(data), `"schemaVersion":3`, `"schemaVersion":99`, 1))
	if _, err := seriesfile.Decode(libRoot, ref, bad); err == nil || !strings.Contains(err.Error(), "schemaVersion") {
		t.Fatalf("Decode err = %v, want schemaVersion error", err)
	}
}

func mustEpisode(t *testing.T, value string) refs.Episode {
	t.Helper()
	ref, err := refs.ParseEpisode(value)
	if err != nil {
		t.Fatalf("ParseEpisode(%q): %v", value, err)
	}
	return ref
}
