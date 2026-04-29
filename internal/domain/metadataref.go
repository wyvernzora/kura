package domain

import "github.com/wyvernzora/kura/internal/metadata"

// MetadataRef identifies a series in an external metadata system.
//
// This type is defined in internal/metadata as it is part of metadata source
// contracts. The domain package keeps this alias for compatibility within
// package-facing call sites.
type MetadataRef = metadata.MetadataRef

// ParseMetadataRef validates and parses `<source>:<id>` style references.
func ParseMetadataRef(value string) (MetadataRef, error) {
	return metadata.ParseMetadataRef(value)
}
