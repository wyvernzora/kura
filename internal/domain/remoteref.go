package domain

import "github.com/wyvernzora/kura/internal/metadata"

// RemoteSeriesRef identifies a series in an external metadata system.
//
// This type is defined in internal/metadata as it is part of provider metadata
// contracts. The domain package keeps this alias for compatibility within
// package-facing call sites.
type RemoteSeriesRef = metadata.RemoteSeriesRef

// ParseRemoteSeriesRef validates and parses `<source>:<id>` style references.
func ParseRemoteSeriesRef(value string) (RemoteSeriesRef, error) {
	return metadata.ParseRemoteSeriesRef(value)
}
