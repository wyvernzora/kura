package kura

import (
	"context"
	"strings"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/ops"
	"github.com/wyvernzora/kura/internal/store"
)

func (s *Series) Stage(ctx context.Context, in StageInput) (StageResult, error) {
	season, err := domain.NewSeasonNumber(in.Season)
	if err != nil {
		return StageResult{}, err
	}
	episode, err := domain.NewEpisodeNumber(in.Episode)
	if err != nil {
		return StageResult{}, err
	}
	source := domain.MediaSource("")
	if strings.TrimSpace(in.Source) != "" {
		source = domain.ParseMediaSource(in.Source)
	}
	result, err := ops.StageEpisodeFile(ctx, s.library.root, string(s.ref), ops.StageEpisodeFileOptions{
		Season:           season,
		Episode:          episode,
		Source:           source,
		Companions:       append([]string(nil), in.Companions...),
		MediaPath:        in.MediaPath,
		Inspector:        s.library.inspector,
		MetadataResolver: s.metadataResolver(),
		Replace:          in.Replace,
	})
	if err != nil {
		return StageResult{}, s.normalizeWorkflowError(err)
	}
	seriesDir := s.library.root.Join(string(s.ref))
	if err := backupBefore(seriesDir, "staged"); err != nil {
		return StageResult{}, err
	}
	if err := store.SaveStaged(result.UpdatedStaged); err != nil {
		return StageResult{}, err
	}
	return StageResult{
		Series:   s.ref,
		Applied:  true,
		Replaced: result.Replaced,
		Entry:    result.Entry,
	}, nil
}
