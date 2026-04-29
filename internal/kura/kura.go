package kura

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/mediainfo"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/metadata/tvdb"
	"github.com/wyvernzora/kura/internal/store"
)

type Config struct {
	Root               string
	MediainfoCommand   string
	TVDBKey            string
	TVDBBaseURL        string
	PreferredLanguages []string
}

type Library struct {
	root           fsroot.LibraryRoot
	metadataSource metadata.Source
	inspector      mediainfo.Inspector
	index          *store.LibraryIndex
}

func New(cfg Config) (*Library, error) {
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

	root, err := fsroot.ParseLibraryRoot(cfg.Root)
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
	index, err := store.LoadLibraryIndex(root)
	if errors.Is(err, store.ErrLibraryIndexNotFound) {
		index, err = store.RebuildLibraryIndex(context.Background(), root)
	}
	if err != nil {
		return nil, err
	}
	return &Library{
		root:           root,
		metadataSource: source,
		inspector:      inspector,
		index:          index,
	}, nil
}
