package scan

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/series/layout"
	"github.com/wyvernzora/kura/internal/series/mediarecord"
	"github.com/wyvernzora/kura/internal/series/state"
)

type Runner struct {
	root      string
	ref       refs.Series
	source    metadata.Source
	inspector media.Inspector
	now       func() time.Time
}

func NewRunner(root string, ref refs.Series, source metadata.Source, inspector media.Inspector, now func() time.Time) Runner {
	return Runner{
		root:      root,
		ref:       ref,
		source:    source,
		inspector: inspector,
		now:       now,
	}
}

func (r Runner) Scan(ctx context.Context, input Input) (Result, error) {
	scanner := newScanner(r, ctx, input)
	if err := scanner.scan(); err != nil {
		return Result{}, err
	}
	return scanner.result, nil
}

type scanner struct {
	runner    Runner
	ctx       context.Context
	input     Input
	model     state.State
	editor    state.Editor
	seriesDir layout.SeriesDir
	result    Result
}

func newScanner(runner Runner, ctx context.Context, input Input) *scanner {
	return &scanner{
		runner: runner,
		ctx:    ctx,
		input:  input,
		result: Result{Series: runner.ref},
	}
}

func (s *scanner) scan() error {
	progress.Start(s.ctx, "scan", fmt.Sprintf("Scanning %s", s.runner.ref), 0)
	if err := s.loadLocal(); err != nil {
		progress.Failure(s.ctx, "scan", fmt.Sprintf("Failed to scan %s", s.runner.ref), 0, 0)
		return err
	}
	if err := s.rejectStagedRecords(); err != nil {
		progress.Failure(s.ctx, "scan", fmt.Sprintf("Failed to scan %s", s.runner.ref), 0, 0)
		return err
	}
	if err := s.refreshMetadata(); err != nil {
		progress.Failure(s.ctx, "scan", fmt.Sprintf("Failed to scan %s", s.runner.ref), 0, 0)
		return err
	}
	progress.Update(s.ctx, "scan", fmt.Sprintf("Discovering files in %s", s.runner.ref), 1, 0)
	discovered, skipped, err := DiscoverSeriesEpisodes(s.seriesDir)
	if err != nil {
		progress.Failure(s.ctx, "scan", fmt.Sprintf("Failed to scan %s", s.runner.ref), 1, 0)
		return err
	}
	s.result.Skipped = skipped
	progress.Update(s.ctx, "scan", fmt.Sprintf("Inspecting %d files", len(discovered)), 2, 0)
	if err := s.apply(discovered); err != nil {
		progress.Failure(s.ctx, "scan", fmt.Sprintf("Failed to scan %s", s.runner.ref), 2, 0)
		return err
	}
	s.model.LastScanned = s.runner.now().UTC()
	if err := state.NewRepository(s.runner.root).Save(s.runner.ref, s.model); err != nil {
		progress.Failure(s.ctx, "scan", fmt.Sprintf("Failed to scan %s", s.runner.ref), 3, 0)
		return err
	}
	progress.Success(s.ctx, "scan", fmt.Sprintf("Scanned %s", s.runner.ref), len(discovered))
	return nil
}

func (s *scanner) loadLocal() error {
	model, err := state.NewRepository(s.runner.root).Load(s.runner.ref)
	if err != nil {
		return err
	}
	seriesDir, err := layout.NewFiles(s.runner.root).SeriesDir(s.runner.ref)
	if err != nil {
		return err
	}
	s.model = model
	s.editor = state.Editor{Series: &s.model}
	s.seriesDir = seriesDir
	return nil
}

func (s *scanner) refreshMetadata() error {
	progress.Update(s.ctx, "scan", fmt.Sprintf("Fetching metadata for %s", s.runner.ref), 0, 0)
	metadataSeries, err := s.runner.source.GetSeries(s.ctx, s.model.Metadata.ID())
	if err != nil {
		return err
	}
	spine, err := spineFromMetadata(metadataSeries.Seasons)
	if err != nil {
		return err
	}
	s.editor.RefreshSpine(spine)
	return nil
}

func (s *scanner) reportInspecting(file DiscoveredFile, current int, total int) {
	progress.Update(s.ctx, "scan", fmt.Sprintf("Inspecting %s", filepath.Base(file.Path)), current, total)
}

func (s *scanner) rejectStagedRecords() error {
	var staged []refs.Episode
	for ref, episode := range s.model.Episodes {
		if episode.Staged != nil {
			staged = append(staged, ref)
		}
	}
	if len(staged) == 0 {
		return nil
	}
	sort.Slice(staged, func(i, j int) bool { return staged[i].String() < staged[j].String() })
	return ScanStagedRecordsError{Episodes: staged}
}

func (s *scanner) mediaRecordBuilder() mediarecord.Builder {
	return mediarecord.NewBuilder(layout.NewFiles(s.runner.root), s.runner.inspector)
}

func spineFromMetadata(seasons []metadata.Season) ([]state.SpineEpisode, error) {
	var spine []state.SpineEpisode
	for _, season := range seasons {
		for _, episode := range season.Episodes {
			if episode.Ref.IsZero() {
				return nil, fmt.Errorf("series: metadata has invalid episode ref")
			}
			spine = append(spine, state.SpineEpisode{Ref: episode.Ref, AirDate: episode.Aired})
		}
	}
	return spine, nil
}
