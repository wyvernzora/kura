package kura

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/ops"
	"github.com/wyvernzora/kura/internal/store"
)

func (l *Library) Add(ctx context.Context, in AddInput) (*Series, error) {
	if in.MetadataRef == "" {
		return nil, errors.New("kura: metadata ref is required")
	}
	meta, parsedRef, err := l.fetchMetadataSeries(ctx, in.MetadataRef)
	if err != nil {
		return nil, err
	}

	ref := in.Ref
	if ref == "" {
		title, err := domain.ParseFileTitle(meta.PreferredTitle)
		if err != nil {
			return nil, err
		}
		ref = SeriesRef(title.String())
	}
	seriesPath, err := domain.ParseSeriesPath(string(ref))
	if err != nil {
		return nil, err
	}
	ref = SeriesRef(seriesPath.String())
	target := l.root.Join(string(ref))
	if _, err := os.Stat(target); err == nil {
		return nil, SeriesAlreadyExistsError{Ref: ref}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := l.checkMetadataRefAvailable(parsedRef, ref); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return nil, err
	}
	seriesDir, err := l.root.SeriesDir(string(ref))
	if err != nil {
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
	if err := l.saveIndexRecord(result.Series, ref); err != nil {
		return nil, err
	}
	return newSeries(l, ref, result.Series), nil
}

func normalizeInitMetadataError(err error, ref domain.MetadataRef) error {
	if ref.Source() != "tvdb" {
		return UnsupportedMetadataSourceError{Source: ref.Source()}
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("kura: metadata ref %s is invalid", ref)
}
