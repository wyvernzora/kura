package series

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/metadata"
)

func TestApplyReconcileTokenRejectsExpiredPlan(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	handle := newReconcilePlanTestHandle(t, &now)

	stored, err := handle.CreateReconcilePlan()
	if err != nil {
		t.Fatalf("CreateReconcilePlan: %v", err)
	}
	if stored.Token == "" {
		t.Fatal("Token is empty")
	}

	now = now.Add(6 * time.Minute)
	_, err = handle.ApplyReconcileToken(context.Background(), stored.Token)
	var expired ReconcilePlanExpiredError
	if !errors.As(err, &expired) {
		t.Fatalf("ApplyReconcileToken error = %v, want ReconcilePlanExpiredError", err)
	}
	if _, err := os.Stat(filepath.Join(handle.root(), "Bookworm", "Season 1", "old episode.mkv")); err != nil {
		t.Fatalf("old file moved despite expired plan: %v", err)
	}
}

func TestCreateReconcilePlanWritesCompactJSONL(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	handle := newReconcilePlanTestHandle(t, &now)

	stored, err := handle.CreateReconcilePlan()
	if err != nil {
		t.Fatalf("CreateReconcilePlan: %v", err)
	}
	path := filepath.Join(handle.root(), "Bookworm", ".kura", "reconcile", stored.Token+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("plan lines = %d, want 1\n%s", len(lines), string(data))
	}
	if strings.Contains(lines[0], "\n  ") || strings.Contains(lines[0], "  \"") {
		t.Fatalf("plan line appears pretty-printed:\n%s", lines[0])
	}
}

func newReconcilePlanTestHandle(t *testing.T, now *time.Time) Handle {
	t.Helper()
	root := t.TempDir()
	seriesDir := filepath.Join(root, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(seasonDir, "old episode.mkv"), []byte("episode"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	episode, err := refs.NewEpisode(1, 1)
	if err != nil {
		t.Fatalf("NewEpisode: %v", err)
	}
	mtime := time.Date(2026, 4, 20, 3, 0, 0, 0, time.UTC)
	state := seriesState{
		Metadata: refs.Metadata("tvdb:370070"),
		Episodes: map[refs.Episode]episodeState{
			episode: {
				AirDate: "2019-10-03",
				Active: &MediaRecord{
					Path:       "Season 1/old episode.mkv",
					Source:     media.SourceWebRip,
					Resolution: mustParseResolution(t, "1920x1080"),
					Codec:      media.Codec("HEVC"),
					Size:       7,
					MTime:      mtime,
					Companions: []CompanionRecord{},
				},
			},
		},
	}
	seriesRef, err := refs.ParseSeries("Bookworm")
	if err != nil {
		t.Fatalf("ParseSeries: %v", err)
	}
	if err := (repo{root: root}).save(seriesRef, state); err != nil {
		t.Fatalf("save: %v", err)
	}
	handle, err := NewHandle(reconcilePlanTestDeps{root: root, now: now}, seriesRef)
	if err != nil {
		t.Fatalf("NewHandle: %v", err)
	}
	return handle
}

type reconcilePlanTestDeps struct {
	root string
	now  *time.Time
}

func (d reconcilePlanTestDeps) LibraryRoot() string {
	return d.root
}

func (d reconcilePlanTestDeps) MetadataSource() metadata.Source {
	return nil
}

func (d reconcilePlanTestDeps) MediaInspector() media.Inspector {
	return nil
}

func (d reconcilePlanTestDeps) Now() time.Time {
	return *d.now
}
