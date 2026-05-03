package mcp

import (
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
	"github.com/wyvernzora/kura/internal/workflow"
)

// resolveSeriesRef looks up the series ref a metadata ref is tracked
// at via the in-memory index. Returns *workflow.MetadataRefNotIndexedError
// on miss so the surface mapper renders kind=not_found,
// category=invalid_params via the Coded interface.
//
// Action tool handlers (everything except kura_resolve and kura_list)
// call this to translate the MetadataRef the agent passed in into
// the SeriesRef workflows take.
func resolveSeriesRef(idx *indexfile.Index, metaRef refs.Metadata) (refs.Series, error) {
	seriesRef, ok, err := idx.Get(metaRef)
	if err != nil {
		return refs.Series{}, err
	}
	if !ok {
		return refs.Series{}, &workflow.MetadataRefNotIndexedError{Ref: metaRef}
	}
	return seriesRef, nil
}
