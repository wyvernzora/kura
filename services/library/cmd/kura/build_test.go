package main

import (
	"context"
	"os"
	"testing"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
)

func mustParseSeries(t *testing.T, name string) refs.Series {
	t.Helper()
	ref, err := refs.ParseSeries(name)
	if err != nil {
		t.Fatalf("ParseSeries(%q): %v", name, err)
	}
	return ref
}

func TestRowBuildOptionsFromEnv(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int
	}{
		{name: "unset", raw: "", want: 7},
		{name: "custom", raw: "14", want: 14},
		{name: "zero", raw: "0", want: 0},
		{name: "invalid", raw: "nope", want: 7},
		{name: "negative", raw: "-1", want: 7},
		{name: "spaces", raw: " 3 ", want: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rowBuildOptionsFromEnv(func(key string) string {
				if key == "KURA_AIRING_TAIL_DAYS" {
					return tt.raw
				}
				return ""
			})
			if got.AiringTailDays != tt.want {
				t.Fatalf("AiringTailDays = %d, want %d", got.AiringTailDays, tt.want)
			}
			if tt.raw == "" && got != indexfile.DefaultBuildOptions() {
				t.Fatalf("default options = %+v, want %+v", got, indexfile.DefaultBuildOptions())
			}
		})
	}
}

func TestLoadOrRebuildIndexRebuildsBuildOptionMismatch(t *testing.T) {
	root := t.TempDir()
	oldOpts := indexfile.DefaultBuildOptions()
	oldOpts.AiringTailDays = 3
	rows := []indexfile.Row{
		{Series: mustParseSeries(t, "Show"), Title: "Show", Status: response.ListStatusComplete},
	}
	if err := indexfile.SaveCASWithOptions(root, "", rows, coord.NewMutator("seed"), oldOpts); err != nil {
		t.Fatalf("SaveCASWithOptions: %v", err)
	}

	idx, err := loadOrRebuildIndex(context.Background(), root, indexfile.DefaultBuildOptions())
	if err != nil {
		t.Fatalf("loadOrRebuildIndex: %v", err)
	}
	if err := idx.WaitReady(context.Background()); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	loaded, err := indexfile.LoadCAS(root)
	if err != nil {
		t.Fatalf("LoadCAS: %v", err)
	}
	if *loaded.Header.BuildOptions != indexfile.DefaultBuildOptions() {
		t.Fatalf("BuildOptions = %+v, want %+v", *loaded.Header.BuildOptions, indexfile.DefaultBuildOptions())
	}
}

func TestLoadOrRebuildIndexColdRebuildStampsBuildOptions(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(root+"/Show", 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	opts := indexfile.DefaultBuildOptions()
	opts.AiringTailDays = 14

	idx, err := loadOrRebuildIndex(context.Background(), root, opts)
	if err != nil {
		t.Fatalf("loadOrRebuildIndex: %v", err)
	}
	if err := idx.WaitReady(context.Background()); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	loaded, err := indexfile.LoadCAS(root)
	if err != nil {
		t.Fatalf("LoadCAS: %v", err)
	}
	if *loaded.Header.BuildOptions != opts {
		t.Fatalf("BuildOptions = %+v, want %+v", *loaded.Header.BuildOptions, opts)
	}
}
