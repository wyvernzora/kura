package main

import (
	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/store"
)

func assertMetadataRefAvailable(rt *runContext, metadataRef string, next domain.SeriesPath) error {
	ref, err := domain.ParseMetadataRef(metadataRef)
	if err != nil {
		return err
	}
	index, err := store.LibraryIndexFrom(rt.Context)
	if err != nil {
		return err
	}
	existingPath, exists, err := index.Get(ref)
	if err != nil {
		return err
	}
	if exists && existingPath.String() != next.String() {
		return store.DuplicateLibraryIndexRefError{Ref: ref, Existing: existingPath, Next: next}
	}
	return nil
}

func updateLibraryIndex(rt *runContext, series store.Series, path domain.SeriesPath) error {
	index, err := store.LibraryIndexFrom(rt.Context)
	if err != nil {
		return err
	}
	if err := index.Put(series, path); err != nil {
		return err
	}
	return index.Save()
}
