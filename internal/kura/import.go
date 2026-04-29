package kura

import (
	"context"
	"errors"
	"os"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/ops"
	"github.com/wyvernzora/kura/internal/store"
)

func (l *Library) Import(ctx context.Context, in ImportInput) (*Series, error) {
	if in.Ref == "" {
		return nil, errors.New("kura: series ref is required")
	}
	if in.MetadataRef == "" {
		return nil, errors.New("kura: metadata ref is required")
	}
	if _, err := domain.ParseSeriesPath(string(in.Ref)); err != nil {
		return nil, err
	}
	meta, parsedRef, err := l.fetchMetadataSeries(ctx, in.MetadataRef)
	if err != nil {
		return nil, err
	}
	seriesDir, err := l.root.SeriesDir(string(in.Ref))
	if errors.Is(err, os.ErrNotExist) {
		return nil, SeriesNotFoundError{Ref: in.Ref}
	}
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(store.SeriesMetadataPath(seriesDir.Path())); err == nil {
		return nil, SeriesAlreadyTrackedError{Ref: in.Ref}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := l.checkMetadataRefAvailable(parsedRef, in.Ref); err != nil {
		return nil, err
	}
	result, err := ops.InitSeries(ops.InitSeriesOptions{SeriesDir: seriesDir, Metadata: meta})
	if err != nil {
		return nil, normalizeInitMetadataError(err, parsedRef)
	}
	if err := store.SaveSeries(result.Series); err != nil {
		return nil, err
	}
	// staged.json and trash.json stay lazy-created; their zero state is
	// equivalent to absence and SaveStaged/SaveTrash remove empty documents.
	if err := l.saveIndexRecord(result.Series, in.Ref); err != nil {
		return nil, err
	}
	return newSeries(l, in.Ref, result.Series), nil
}
