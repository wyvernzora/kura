package kura

import (
	"github.com/wyvernzora/kura/internal/refs"
)

type ResolveInput struct {
	Terms []string
}

type AddInput struct {
	MetadataRef refs.Metadata
	Ref         refs.Series
}

type ImportInput struct {
	Ref         refs.Series
	MetadataRef refs.Metadata
}
