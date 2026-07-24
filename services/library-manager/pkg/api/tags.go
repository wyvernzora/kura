package api

import "github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"

// TagUpdate is the request body shared by REST and transport clients.
// Plain expressions add a tag; expressions prefixed with ! remove it.
type TagUpdate struct {
	Tags []string `json:"tags"`
}

// SeriesTags is the resulting stored tag set after an update.
type SeriesTags struct {
	MetadataRef refs.Metadata `json:"metadataRef"`
	Tags        []string      `json:"tags"`
}
