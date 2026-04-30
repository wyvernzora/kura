package kura

import (
	"errors"

	"github.com/wyvernzora/kura/internal/resolve"
)

var (
	ErrRootNotFound     = errors.New("kura: library root does not exist")
	ErrRootNotDirectory = errors.New("kura: library root is not a directory")
	ErrMissingTVDBKey   = errors.New("kura: tvdb api key is required")
)

var (
	ErrEmptyQuery       = resolve.ErrEmptyQuery
	ErrTooManyTerms     = resolve.ErrTooManyTerms
	ErrConflictingTerms = resolve.ErrConflictingTerms
	ErrNoStrategyMatch  = resolve.ErrNoStrategyMatch
)
