package series

import "context"

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
	if err := s.load(); err != nil {
		return err
	}
	discovered, skipped, err := discoverSeriesEpisodes(s.seriesDir)
	if err != nil {
		return err
	}
	s.result.Skipped = skipped
	if err := s.apply(discovered); err != nil {
		return err
	}
	s.model.LastScanned = s.handle.now().UTC()
	return s.handle.repo().save(s.handle.ref, s.model)
}

func (s *scanner) load() error {
	model, err := s.handle.load()
	if err != nil {
		return err
	}
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
