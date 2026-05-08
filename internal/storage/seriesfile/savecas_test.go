package seriesfile_test

import (
	"errors"
	"os"
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

func textnormFor(_ *testing.T, value string) textnorm.NFCString {
	return textnorm.NFC(value)
}

func TestLoadPopulatesHash(t *testing.T) {
	libRoot, ref := setupFixtureLibrary(t)
	model, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(model.Hash) != 64 {
		t.Fatalf("Hash = %q, want 64-char hex", model.Hash)
	}
}

func TestSaveCASRoundtripPreservesHash(t *testing.T) {
	libRoot, ref := setupFixtureLibrary(t)
	model, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	original := model.Hash

	// Mutate something so the new hash differs.
	model.PreferredTitle = textnormFor(t, "Bookworm Updated")

	mutator := coord.Mutator{
		Op:   "stage",
		PID:  12345,
		Host: "workstation",
		At:   time.Date(2026, 5, 2, 19, 14, 0, 0, time.UTC),
	}
	if err := seriesfile.SaveCAS(libRoot, model, mutator); err != nil {
		t.Fatalf("SaveCAS: %v", err)
	}
	if model.Hash == original {
		t.Fatal("Hash unchanged after mutation + SaveCAS")
	}

	reloaded, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Hash != model.Hash {
		t.Fatalf("reloaded hash = %q, written = %q", reloaded.Hash, model.Hash)
	}
	if reloaded.LastMutated.Op == "" {
		t.Fatal("LastMutated not persisted")
	}
	if reloaded.LastMutated.Op != "stage" || reloaded.LastMutated.PID != 12345 {
		t.Fatalf("LastMutated = %+v", reloaded.LastMutated)
	}
}

func TestSaveCASDetectsPreWriteDrift(t *testing.T) {
	libRoot, ref := setupFixtureLibrary(t)
	model, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Simulate a peer mutation between our load and our save.
	peer := *model
	peer.PreferredTitle = textnormFor(t, "Peer Won")
	if err := seriesfile.SaveCAS(libRoot, &peer, coord.Mutator{
		Op:   "scan",
		PID:  999,
		Host: "workstation",
		At:   time.Date(2026, 5, 2, 19, 14, 30, 0, time.UTC),
	}); err != nil {
		t.Fatalf("peer SaveCAS: %v", err)
	}

	// Our save should now conflict — model.Hash matches the pre-peer state.
	model.PreferredTitle = textnormFor(t, "We Tried")
	err = seriesfile.SaveCAS(libRoot, model, coord.Mutator{Op: "stage", PID: 1, Host: "workstation", At: time.Now()})
	var conflict *coord.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want ConflictError", err)
	}
	if conflict.Phase != "pre_write" {
		t.Fatalf("Phase = %q, want pre_write", conflict.Phase)
	}
	if conflict.Mutator.Op != "scan" || conflict.Mutator.PID != 999 {
		t.Fatalf("conflict mutator = %+v, want winning peer's", conflict.Mutator)
	}
}

func TestSaveCASCreateExclusive(t *testing.T) {
	libRoot := t.TempDir()
	ref, _ := refs.ParseSeries("NewShow")
	model := &series.Series{
		Ref:      ref,
		Metadata: refs.Metadata("tvdb:42"),
		Episodes: map[refs.Episode]series.Episode{},
		// Hash empty → O_EXCL create.
	}
	mutator := coord.Mutator{Op: "add", PID: 1, Host: "h", At: time.Now()}
	if err := seriesfile.SaveCAS(libRoot, model, mutator); err != nil {
		t.Fatalf("first SaveCAS: %v", err)
	}
	if model.Hash == "" {
		t.Fatal("Hash not populated after create")
	}

	// Second create attempt with empty Hash should conflict.
	model2 := &series.Series{
		Ref:      ref,
		Metadata: refs.Metadata("tvdb:42"),
		Episodes: map[refs.Episode]series.Episode{},
	}
	err := seriesfile.SaveCAS(libRoot, model2, mutator)
	var conflict *coord.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("second create err = %v, want ConflictError", err)
	}
}

func TestSaveCASCreate_AtomicityAndCleanLayout(t *testing.T) {
	libRoot := t.TempDir()
	ref, _ := refs.ParseSeries("AtomicShow")
	model := &series.Series{
		Ref:      ref,
		Metadata: refs.Metadata("tvdb:1"),
		Episodes: map[refs.Episode]series.Episode{},
	}
	mutator := coord.Mutator{Op: "add", PID: 1, Host: "h", At: time.Now()}
	if err := seriesfile.SaveCAS(libRoot, model, mutator); err != nil {
		t.Fatalf("SaveCAS: %v", err)
	}

	// Roundtrip Load: file is complete (no truncation).
	reloaded, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.Hash != model.Hash {
		t.Fatalf("Hash mismatch: reload=%q write=%q", reloaded.Hash, model.Hash)
	}

	// .kura dir must contain only series.json — no leftover renameio temps.
	entries, err := os.ReadDir(paths.SeriesKuraDir(libRoot, ref))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "series.json" {
			t.Errorf("unexpected leftover in .kura: %s", e.Name())
		}
	}
}

func TestSaveCASCreate_PreExistingFileConflicts(t *testing.T) {
	libRoot := t.TempDir()
	ref, _ := refs.ParseSeries("PreExisting")

	// Plant a file directly at the canonical path (simulating an earlier
	// successful create from any source).
	kuraDir := paths.SeriesKuraDir(libRoot, ref)
	if err := os.MkdirAll(kuraDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.SeriesMetadata(libRoot, ref), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	model := &series.Series{
		Ref:      ref,
		Metadata: refs.Metadata("tvdb:2"),
		Episodes: map[refs.Episode]series.Episode{},
		// Empty Hash → create path.
	}
	err := seriesfile.SaveCAS(libRoot, model, coord.Mutator{Op: "add", PID: 1, Host: "h", At: time.Now()})
	var conflict *coord.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want *coord.ConflictError", err)
	}
	if conflict.Phase != "pre_write" {
		t.Fatalf("Phase = %q, want pre_write", conflict.Phase)
	}
}

func TestSaveCASInProgressClaim(t *testing.T) {
	libRoot, ref := setupFixtureLibrary(t)
	model, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	holder := coord.Holder{
		Op:      "reconcile_apply",
		Token:   "abc123def456",
		PID:     12345,
		Host:    "workstation",
		Started: time.Date(2026, 5, 2, 19, 14, 33, 0, time.UTC),
	}
	model.InProgress = &holder
	if err := seriesfile.SaveCAS(libRoot, model, coord.Mutator{Op: "reconcile_apply_claim", PID: 12345, Host: "workstation", At: time.Now()}); err != nil {
		t.Fatalf("SaveCAS with claim: %v", err)
	}

	reloaded, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.InProgress == nil {
		t.Fatal("InProgress not persisted")
	}
	if reloaded.InProgress.Token != "abc123def456" {
		t.Fatalf("Token = %q", reloaded.InProgress.Token)
	}

	// Clear claim by setting InProgress = nil.
	reloaded.InProgress = nil
	if err := seriesfile.SaveCAS(libRoot, reloaded, coord.Mutator{Op: "reconcile_apply", PID: 12345, Host: "workstation", At: time.Now()}); err != nil {
		t.Fatalf("SaveCAS clear claim: %v", err)
	}
	cleared, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("reload after clear: %v", err)
	}
	if cleared.InProgress != nil {
		t.Fatalf("InProgress = %+v, want nil after clear", cleared.InProgress)
	}
}

func TestSaveCASMissingFileSurfaces(t *testing.T) {
	libRoot := t.TempDir()
	ref, _ := refs.ParseSeries("NotThere")
	model := &series.Series{
		Ref:      ref,
		Metadata: refs.Metadata("tvdb:1"),
		Episodes: map[refs.Episode]series.Episode{},
		Hash:     "deadbeef" + strings.Repeat("0", 56),
	}
	err := seriesfile.SaveCAS(libRoot, model, coord.Mutator{Op: "stage", PID: 1, Host: "h", At: time.Now()})
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want os.ErrNotExist", err)
	}
}

func TestSaveCASRejectsNilOrZeroRef(t *testing.T) {
	libRoot := t.TempDir()
	mutator := coord.Mutator{Op: "stage", PID: 1, Host: "h", At: time.Now()}

	if err := seriesfile.SaveCAS(libRoot, nil, mutator); err == nil {
		t.Fatal("SaveCAS(nil) returned nil error")
	}
	model := &series.Series{Metadata: refs.Metadata("tvdb:1")}
	if err := seriesfile.SaveCAS(libRoot, model, mutator); err == nil || !strings.Contains(err.Error(), "zero Ref") {
		t.Fatalf("zero ref err = %v", err)
	}
}

func TestSaveCASRoundtripsOrdering(t *testing.T) {
	libRoot, ref := setupFixtureLibrary(t)
	model, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if model.Ordering != "" {
		t.Fatalf("baseline Ordering = %q, want empty", model.Ordering)
	}
	model.Ordering = "dvd"
	if err := seriesfile.SaveCAS(libRoot, model, coord.Mutator{Op: "scan", PID: 1, Host: "h", At: time.Now().UTC()}); err != nil {
		t.Fatalf("SaveCAS: %v", err)
	}
	reloaded, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Ordering != "dvd" {
		t.Fatalf("Ordering = %q, want %q", reloaded.Ordering, "dvd")
	}

	reloaded.Ordering = ""
	if err := seriesfile.SaveCAS(libRoot, reloaded, coord.Mutator{Op: "scan", PID: 1, Host: "h", At: time.Now().UTC()}); err != nil {
		t.Fatalf("SaveCAS clear: %v", err)
	}
	cleared, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("reload cleared: %v", err)
	}
	if cleared.Ordering != "" {
		t.Fatalf("Ordering after clear = %q, want empty", cleared.Ordering)
	}
}

func TestStagedTrashRoundtrip(t *testing.T) {
	libRoot, ref := setupFixtureLibrary(t)
	model, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(model.StagedTrash) != 0 {
		t.Fatalf("baseline StagedTrash len = %d, want 0", len(model.StagedTrash))
	}

	id := ulid.Make()
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	model.AddStagedTrash(series.StagedTrashItem{
		ID:      id,
		Path:    "/abs/" + ref.String() + "/Season 1/loser.mkv",
		Size:    12345,
		MTime:   now.Add(-time.Hour),
		AddedAt: now,
		Companions: []media.Companion{{
			Path:  "/abs/" + ref.String() + "/Season 1/loser.srt",
			Size:  321,
			MTime: now.Add(-time.Hour),
		}},
	})
	if err := seriesfile.SaveCAS(libRoot, model, coord.Mutator{Op: "stage", PID: 1, Host: "h", At: now}); err != nil {
		t.Fatalf("SaveCAS: %v", err)
	}
	reloaded, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(reloaded.StagedTrash) != 1 {
		t.Fatalf("reloaded StagedTrash len = %d, want 1", len(reloaded.StagedTrash))
	}
	got := reloaded.StagedTrash[0]
	if got.ID != id {
		t.Fatalf("ID = %s, want %s", got.ID, id)
	}
	if got.Size != 12345 {
		t.Fatalf("Size = %d, want 12345", got.Size)
	}
	// Path absolutized on Load.
	if got.Path == "" || got.Path[0] != '/' {
		t.Fatalf("Path = %q, want absolute", got.Path)
	}
	if len(got.Companions) != 1 {
		t.Fatalf("Companions len = %d, want 1", len(got.Companions))
	}

	// Round-trip clear.
	reloaded.ClearStagedTrash()
	if err := seriesfile.SaveCAS(libRoot, reloaded, coord.Mutator{Op: "reset", PID: 1, Host: "h", At: now}); err != nil {
		t.Fatalf("SaveCAS clear: %v", err)
	}
	cleared, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("reload cleared: %v", err)
	}
	if len(cleared.StagedTrash) != 0 {
		t.Fatalf("cleared StagedTrash len = %d, want 0", len(cleared.StagedTrash))
	}
	// stagedTrash is required on the v3 wire shape; cleared state
	// surfaces as the empty array, never as a missing field.
	bytes, err := os.ReadFile(paths.SeriesMetadata(libRoot, ref))
	if err != nil {
		t.Fatalf("read on-disk: %v", err)
	}
	if !strings.Contains(string(bytes), `"stagedTrash": []`) {
		t.Fatalf("on-disk file missing empty stagedTrash array after clear:\n%s", bytes)
	}
}

func TestStagedExtrasRoundtrip(t *testing.T) {
	libRoot, ref := setupFixtureLibrary(t)
	model, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(model.StagedExtras) != 0 {
		t.Fatalf("baseline StagedExtras len = %d, want 0", len(model.StagedExtras))
	}

	id := ulid.Make()
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	model.AddStagedExtra(series.StagedExtraItem{
		ID:      id,
		Season:  1,
		Path:    "/abs/elsewhere/specials",
		Prefix:  "behind-the-scenes",
		IsDir:   true,
		AddedAt: now,
	})
	if err := seriesfile.SaveCAS(libRoot, model, coord.Mutator{Op: "stage", PID: 1, Host: "h", At: now}); err != nil {
		t.Fatalf("SaveCAS: %v", err)
	}
	reloaded, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(reloaded.StagedExtras) != 1 {
		t.Fatalf("reloaded StagedExtras len = %d, want 1", len(reloaded.StagedExtras))
	}
	got := reloaded.StagedExtras[0]
	if got.ID != id || got.Season != 1 || got.Prefix != "behind-the-scenes" {
		t.Fatalf("StagedExtras[0] = %+v", got)
	}
	if got.Path != "/abs/elsewhere/specials" {
		t.Fatalf("Path = %q, want unchanged absolute /abs/elsewhere/specials", got.Path)
	}
	if !got.IsDir {
		t.Fatalf("IsDir = false, want true (persisted)")
	}

	// Round-trip clear; field absent on disk.
	reloaded.ClearStagedExtras()
	if err := seriesfile.SaveCAS(libRoot, reloaded, coord.Mutator{Op: "reset", PID: 1, Host: "h", At: now}); err != nil {
		t.Fatalf("SaveCAS clear: %v", err)
	}
	bytes, err := os.ReadFile(paths.SeriesMetadata(libRoot, ref))
	if err != nil {
		t.Fatalf("read on-disk: %v", err)
	}
	if !strings.Contains(string(bytes), `"stagedExtras": []`) {
		t.Fatalf("on-disk file missing empty stagedExtras array after clear:\n%s", bytes)
	}
}

func TestLoadFixtureCarriesClaimFields(t *testing.T) {
	libRoot, ref := setupFixtureLibrary(t)
	// Fixture sets last_mutated; schema requires it. InProgress stays
	// nullable for series with no live claim.
	model, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if model.InProgress != nil {
		t.Fatalf("InProgress = %+v, want nil for fixture without live claim", model.InProgress)
	}
	if model.LastMutated.Op == "" {
		t.Fatal("LastMutated not loaded from fixture")
	}
	// SaveCAS overwrites last_mutated with the new mutator and reloads
	// preserve it byte-stable.
	if err := seriesfile.SaveCAS(libRoot, model, coord.Mutator{Op: "stage", PID: 1, Host: "h", At: time.Now()}); err != nil {
		t.Fatalf("SaveCAS: %v", err)
	}
	reloaded, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.LastMutated.Op != "stage" {
		t.Fatalf("LastMutated.Op = %q, want stage after SaveCAS", reloaded.LastMutated.Op)
	}
}
