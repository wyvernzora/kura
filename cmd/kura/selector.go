package main

import "github.com/wyvernzora/kura/internal/library"

func resolveSeriesSelector(root library.LibraryRoot, series string) (library.SeriesDir, error) {
	// TODO: Replace direct child directory lookup with library-wide series selector
	// resolution once Kura has an index of local series metadata.
	return root.SeriesDir(series)
}
