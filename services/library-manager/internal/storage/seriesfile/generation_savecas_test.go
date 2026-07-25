package seriesfile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/services/library-manager/internal/coord"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/media"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/series"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/paths"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/seriesfile"
	"github.com/wyvernzora/kura/services/library-manager/internal/textnorm"
)

func TestGenerationBumpMatrix(t *testing.T) {
	episode1 := mustEpisode(t, "S01E0001")
	episode2 := mustEpisode(t, "S01E0002")
	episode3 := mustEpisode(t, "S01E0003")
	episode4 := mustEpisode(t, "S01E0004")
	newResolution, err := media.ParseResolution("3840x2160")
	if err != nil {
		t.Fatalf("ParseResolution: %v", err)
	}
	newAirDate, err := series.ParseAirDate("2019-10-10")
	if err != nil {
		t.Fatalf("ParseAirDate: %v", err)
	}
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		prepare  func(*series.Series)
		mutate   func(*series.Series)
		wantBump bool
	}{
		{
			name: "active record added",
			mutate: func(model *series.Series) {
				root := filepath.Dir(filepath.Dir(model.Episodes[episode1].Active.Path))
				episode := model.Episodes[episode3]
				episode.Active = &media.Record{
					Path:       filepath.Join(root, "Season 1", "Bookworm - S01E03.mkv"),
					Source:     media.SourceWebDL,
					Size:       300,
					MTime:      now,
					Companions: []media.Companion{},
				}
				model.Episodes[episode3] = episode
			},
			wantBump: true,
		},
		{
			name: "active record removed",
			mutate: func(model *series.Series) {
				episode := model.Episodes[episode1]
				episode.Active = nil
				model.Episodes[episode1] = episode
			},
			wantBump: true,
		},
		{
			name: "active path changed",
			mutate: func(model *series.Series) {
				model.Episodes[episode1].Active.Path = filepath.Join(
					filepath.Dir(model.Episodes[episode1].Active.Path),
					"replacement.mkv",
				)
			},
			wantBump: true,
		},
		{
			name: "active size changed",
			mutate: func(model *series.Series) {
				model.Episodes[episode1].Active.Size++
			},
			wantBump: true,
		},
		{
			name: "active mtime changed",
			mutate: func(model *series.Series) {
				model.Episodes[episode1].Active.MTime = now
			},
			wantBump: true,
		},
		{
			name: "active source changed",
			mutate: func(model *series.Series) {
				model.Episodes[episode1].Active.Source = media.SourceWebDL
			},
			wantBump: true,
		},
		{
			name: "active attrs changed",
			mutate: func(model *series.Series) {
				model.Episodes[episode1].Active.Attrs = media.Attrs{"origin": "takuhai"}
			},
			wantBump: true,
		},
		{
			name: "companion added",
			mutate: func(model *series.Series) {
				active := model.Episodes[episode1].Active
				active.Companions = append(active.Companions, media.Companion{
					Path:  filepath.Join(filepath.Dir(active.Path), "signs.ass"),
					Size:  200,
					MTime: now,
				})
			},
			wantBump: true,
		},
		{
			name: "companion path changed",
			mutate: func(model *series.Series) {
				active := model.Episodes[episode1].Active
				active.Companions[0].Path = filepath.Join(filepath.Dir(active.Path), "renamed.srt")
			},
			wantBump: true,
		},
		{
			name: "companion size changed",
			mutate: func(model *series.Series) {
				model.Episodes[episode1].Active.Companions[0].Size++
			},
			wantBump: true,
		},
		{
			name: "companion mtime changed",
			mutate: func(model *series.Series) {
				model.Episodes[episode1].Active.Companions[0].MTime = now
			},
			wantBump: true,
		},
		{
			name: "metadata ref changed",
			mutate: func(model *series.Series) {
				model.Metadata = refs.Metadata("tvdb:370071")
			},
			wantBump: true,
		},
		{
			name: "ordering changed",
			mutate: func(model *series.Series) {
				model.Ordering = "dvd"
			},
			wantBump: true,
		},
		{
			name: "resolution changed",
			mutate: func(model *series.Series) {
				model.Episodes[episode1].Active.Resolution = newResolution
			},
		},
		{
			name: "codec changed",
			mutate: func(model *series.Series) {
				model.Episodes[episode1].Active.Codec = media.ParseCodec("AV1")
			},
		},
		{
			name: "companion role changed",
			mutate: func(model *series.Series) {
				model.Episodes[episode1].Active.Companions[0].Role = "subtitle"
			},
		},
		{
			name: "companion language changed",
			mutate: func(model *series.Series) {
				model.Episodes[episode1].Active.Companions[0].Language = "ja"
			},
		},
		{
			name: "companion label changed",
			mutate: func(model *series.Series) {
				model.Episodes[episode1].Active.Companions[0].Label = "signs"
			},
		},
		{
			name: "preferred title changed",
			mutate: func(model *series.Series) {
				model.PreferredTitle = textnorm.NFC("Ascendance of a Bookworm")
			},
		},
		{
			name: "canonical title changed",
			mutate: func(model *series.Series) {
				model.CanonicalTitle = textnorm.NFC("Honzuki no Gekokujou")
			},
		},
		{
			name: "artwork changed",
			mutate: func(model *series.Series) {
				model.Artwork.Poster = series.Poster{
					URL:          "https://example.com/poster.jpg",
					ThumbnailURL: "https://example.com/poster-thumb.jpg",
					Language:     "ja",
				}
			},
		},
		{
			name: "episode air date changed",
			mutate: func(model *series.Series) {
				episode := model.Episodes[episode1]
				episode.AirDate = newAirDate
				model.Episodes[episode1] = episode
			},
		},
		{
			name: "episode titles changed",
			mutate: func(model *series.Series) {
				episode := model.Episodes[episode1]
				episode.PreferredTitle = textnorm.NFC("Episode One")
				episode.CanonicalTitle = textnorm.NFC("First Episode")
				model.Episodes[episode1] = episode
			},
		},
		{
			name: "tag added",
			mutate: func(model *series.Series) {
				model.Tags = []string{"priority:high"}
			},
		},
		{
			name: "user alias added",
			mutate: func(model *series.Series) {
				model.UserAliases = []textnorm.NFCString{textnorm.NFC("bookworm")}
			},
		},
		{
			name: "search key changed",
			mutate: func(model *series.Series) {
				model.SearchKey = "changed"
			},
		},
		{
			name: "staged record set",
			mutate: func(model *series.Series) {
				episode := model.Episodes[episode3]
				episode.Staged = &media.Record{
					Path:       "/inbox/Bookworm S01E03.mkv",
					Source:     media.SourceWebDL,
					Size:       300,
					MTime:      now,
					Companions: []media.Companion{},
				}
				model.Episodes[episode3] = episode
			},
		},
		{
			name: "staged record cleared",
			mutate: func(model *series.Series) {
				episode := model.Episodes[episode2]
				episode.Staged = nil
				model.Episodes[episode2] = episode
			},
		},
		{
			name: "staged trash changed",
			mutate: func(model *series.Series) {
				root := filepath.Dir(filepath.Dir(model.Episodes[episode1].Active.Path))
				model.StagedTrash = []series.StagedTrashItem{{
					ID:      ulid.MustParse("01ARYZ6S410000000000000000"),
					Path:    filepath.Join(root, "Season 1", "old.mkv"),
					Size:    100,
					MTime:   now,
					AddedAt: now,
				}}
			},
		},
		{
			name: "staged extras changed",
			mutate: func(model *series.Series) {
				model.StagedExtras = []series.StagedExtraItem{{
					ID:      ulid.MustParse("01ARYZ6S410000000000000001"),
					Season:  1,
					Path:    "inbox:extras/booklet.pdf",
					AddedAt: now,
				}}
			},
		},
		{
			name: "empty spine slot added",
			mutate: func(model *series.Series) {
				model.Episodes[episode4] = series.Episode{}
			},
		},
		{
			name: "empty spine slot removed",
			mutate: func(model *series.Series) {
				delete(model.Episodes, episode3)
			},
		},
		{
			name: "in progress set",
			mutate: func(model *series.Series) {
				model.InProgress = &coord.Holder{
					Op:      "reconcile_apply",
					Token:   "token",
					PID:     1,
					Host:    "host",
					Started: now,
				}
			},
		},
		{
			name: "in progress cleared",
			prepare: func(model *series.Series) {
				model.InProgress = &coord.Holder{
					Op:      "reconcile_apply",
					Token:   "token",
					PID:     1,
					Host:    "host",
					Started: now,
				}
			},
			mutate: func(model *series.Series) {
				model.InProgress = nil
			},
		},
		{
			name: "last scanned changed",
			mutate: func(model *series.Series) {
				model.LastScanned = now
			},
		},
		{
			name: "date added changed",
			mutate: func(model *series.Series) {
				model.DateAdded = now
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			libRoot, ref := setupFixtureLibrary(t)
			model, err := seriesfile.Load(libRoot, ref)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if err := seriesfile.SaveCAS(libRoot, model, coord.NewMutator("normalize_fixture")); err != nil {
				t.Fatalf("normalize fixture SaveCAS: %v", err)
			}
			model, err = seriesfile.Load(libRoot, ref)
			if err != nil {
				t.Fatalf("reload normalized fixture: %v", err)
			}
			if tt.prepare != nil {
				tt.prepare(model)
				if err := seriesfile.SaveCAS(libRoot, model, coord.NewMutator("prepare")); err != nil {
					t.Fatalf("prepare SaveCAS: %v", err)
				}
				model, err = seriesfile.Load(libRoot, ref)
				if err != nil {
					t.Fatalf("reload after prepare: %v", err)
				}
			}
			before := model.Generation
			tt.mutate(model)
			if err := seriesfile.SaveCAS(libRoot, model, coord.NewMutator("generation_test")); err != nil {
				t.Fatalf("SaveCAS: %v", err)
			}
			want := before
			if tt.wantBump {
				want++
			}
			if model.Generation != want {
				t.Fatalf("model generation = %d, want %d", model.Generation, want)
			}
			reloaded, err := seriesfile.Load(libRoot, ref)
			if err != nil {
				t.Fatalf("reload: %v", err)
			}
			if reloaded.Generation != want {
				t.Fatalf("reloaded generation = %d, want %d", reloaded.Generation, want)
			}
		})
	}
}

func TestSaveCASCreateStartsAtGeneration1(t *testing.T) {
	libRoot := t.TempDir()
	ref, err := refs.ParseSeries("New Show")
	if err != nil {
		t.Fatalf("ParseSeries: %v", err)
	}
	model := &series.Series{
		Ref:      ref,
		Metadata: refs.Metadata("tvdb:42"),
		Episodes: map[refs.Episode]series.Episode{},
	}
	if err := seriesfile.SaveCAS(libRoot, model, coord.NewMutator("add")); err != nil {
		t.Fatalf("SaveCAS: %v", err)
	}
	if model.Generation != 1 {
		t.Fatalf("model generation = %d, want 1", model.Generation)
	}
	reloaded, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.Generation != 1 {
		t.Fatalf("reloaded generation = %d, want 1", reloaded.Generation)
	}
}

func TestGenerationNeverDecreasesAcrossChurnOnlySaves(t *testing.T) {
	libRoot, ref := setupFixtureLibrary(t)
	model, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	episode1 := mustEpisode(t, "S01E0001")
	model.Episodes[episode1].Active.Size++
	if err := seriesfile.SaveCAS(libRoot, model, coord.NewMutator("content_change")); err != nil {
		t.Fatalf("content SaveCAS: %v", err)
	}
	if model.Generation != 2 {
		t.Fatalf("generation after content change = %d, want 2", model.Generation)
	}

	mutations := []func(*series.Series){
		func(current *series.Series) { current.PreferredTitle = textnorm.NFC("New Title") },
		func(current *series.Series) { current.Tags = []string{"priority:low"} },
		func(current *series.Series) { current.LastScanned = current.LastScanned.Add(time.Minute) },
	}
	for i, mutate := range mutations {
		mutate(model)
		if err := seriesfile.SaveCAS(libRoot, model, coord.NewMutator("churn")); err != nil {
			t.Fatalf("churn SaveCAS %d: %v", i, err)
		}
		if model.Generation != 2 {
			t.Fatalf("generation after churn save %d = %d, want 2", i, model.Generation)
		}
	}
}

func TestSaveCASSerializesGeneration2(t *testing.T) {
	libRoot, ref := setupFixtureLibrary(t)
	model, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	episode1 := mustEpisode(t, "S01E0001")
	model.Episodes[episode1].Active.Size++
	if err := seriesfile.SaveCAS(libRoot, model, coord.NewMutator("scan")); err != nil {
		t.Fatalf("SaveCAS: %v", err)
	}
	data, err := os.ReadFile(paths.SeriesMetadata(libRoot, ref))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), `"generation": 2`) {
		t.Fatalf("saved series.json missing generation 2:\n%s", data)
	}
}
