package library

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/wyvernzora/kura/internal/media/mediainfo"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/metadata/tvdb"
	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/series"
)

type Config struct {
	Context            context.Context
	Root               string
	MediainfoCommand   string
	TVDBKey            string
	TVDBBaseURL        string
	PreferredLanguages []string
}

func Open(cfg Config) (*Library, error) {
	ctx := cfg.Context
	if ctx == nil {
		ctx = context.Background()
	}
	info, err := os.Stat(cfg.Root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrRootNotFound
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, ErrRootNotDirectory
	}
	if strings.TrimSpace(cfg.TVDBKey) == "" {
		return nil, ErrMissingTVDBKey
	}

	root, err := ParseRoot(cfg.Root)
	if err != nil {
		return nil, err
	}
	provider, err := tvdb.New(cfg.TVDBKey, tvdb.Options{
		BaseURL:            cfg.TVDBBaseURL,
		PreferredLanguages: cfg.PreferredLanguages,
	})
	if err != nil {
		return nil, err
	}
	inspector := mediainfo.New()
	if strings.TrimSpace(cfg.MediainfoCommand) != "" {
		inspector.Command = strings.TrimSpace(cfg.MediainfoCommand)
	}
	source, err := metadata.NewCachedSource(provider, metadata.CacheOptions{})
	if err != nil {
		return nil, err
	}
	index, err := LoadIndex(root)
	if errors.Is(err, ErrNotFound) {
		index, err = RebuildIndex(ctx, root, func(_ context.Context, ref refs.Series) (refs.Metadata, error) {
			return series.ReadMetadataRef(root.Path(), ref)
		})
	} else if err != nil {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	return New(root, source, inspector, index), nil
}
