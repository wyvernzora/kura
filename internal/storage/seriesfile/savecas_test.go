package seriesfile_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/domain/series"
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
	if reloaded.LastMutated == nil {
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

func TestLoadLegacyFileWithoutClaimFields(t *testing.T) {
	libRoot, ref := setupFixtureLibrary(t)
	// Fixture has no in_progress / last_mutated. Should load cleanly.
	model, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if model.InProgress != nil {
		t.Fatalf("InProgress = %+v, want nil for legacy file", model.InProgress)
	}
	if model.LastMutated != nil {
		t.Fatalf("LastMutated = %+v, want nil for legacy file", model.LastMutated)
	}
	// And SaveCAS through populates them, on reload they're present.
	if err := seriesfile.SaveCAS(libRoot, model, coord.Mutator{Op: "stage", PID: 1, Host: "h", At: time.Now()}); err != nil {
		t.Fatalf("SaveCAS: %v", err)
	}
	reloaded, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.LastMutated == nil {
		t.Fatal("LastMutated not present after first SaveCAS")
	}
}
