package series

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/refs"
)

type scanner struct {
	handle    Handle
	ctx       context.Context
	input     ScanInput
	model     seriesState
	editor    editor
	seriesDir SeriesDir
	result    ScanResult
}

func newScanner(handle Handle, ctx context.Context, input ScanInput) *scanner {
	return &scanner{
		handle: handle,
		ctx:    ctx,
		input:  input,
		result: ScanResult{Series: handle.ref},
	}
}

func (s *scanner) scan() error {
	progress.Start(s.ctx, "scan", fmt.Sprintf("Scanning %s", s.handle.ref), 0)
	if err := s.load(); err != nil {
		progress.Failure(s.ctx, "scan", fmt.Sprintf("Failed to scan %s", s.handle.ref), 0, 0)
		return err
	}
	if err := s.rejectStagedRecords(); err != nil {
		progress.Failure(s.ctx, "scan", fmt.Sprintf("Failed to scan %s", s.handle.ref), 0, 0)
		return err
	}
	progress.Update(s.ctx, "scan", fmt.Sprintf("Discovering files in %s", s.handle.ref), 1, 0)
	discovered, skipped, err := discoverSeriesEpisodes(s.seriesDir)
	if err != nil {
		progress.Failure(s.ctx, "scan", fmt.Sprintf("Failed to scan %s", s.handle.ref), 1, 0)
		return err
	}
	s.result.Skipped = skipped
	progress.Update(s.ctx, "scan", fmt.Sprintf("Inspecting %d files", len(discovered)), 2, 0)
	if err := s.apply(discovered); err != nil {
		progress.Failure(s.ctx, "scan", fmt.Sprintf("Failed to scan %s", s.handle.ref), 2, 0)
		return err
	}
	s.model.LastScanned = s.handle.now().UTC()
	if err := s.handle.repo().save(s.handle.ref, s.model); err != nil {
		progress.Failure(s.ctx, "scan", fmt.Sprintf("Failed to scan %s", s.handle.ref), 3, 0)
		return err
	}
	progress.Success(s.ctx, "scan", fmt.Sprintf("Scanned %s", s.handle.ref), len(discovered))
	return nil
}

func (s *scanner) load() error {
	model, err := s.handle.load()
	if err != nil {
		return err
	}
	progress.Update(s.ctx, "scan", fmt.Sprintf("Fetching metadata for %s", s.handle.ref), 0, 0)
	metadataSeries, err := s.handle.source().GetSeries(s.ctx, model.Metadata.ID())
	if err != nil {
		return err
	}
	spine, err := spineFromMetadata(metadataSeries.Seasons)
	if err != nil {
		return err
	}
	seriesDir, err := s.handle.files().seriesDir(s.handle.ref)
	if err != nil {
		return err
	}
	s.model = model
	s.editor = editor{series: &s.model}
	s.editor.refreshSpine(spine)
	s.seriesDir = seriesDir
	return nil
}

func (s *scanner) reportInspecting(file discoveredFile, current int, total int) {
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
