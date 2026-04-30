package library

import (
	"errors"

	"github.com/wyvernzora/kura/internal/resolve"
)

var (
	ErrRootNotFound     = errors.New("library: root does not exist")
	ErrRootNotDirectory = errors.New("library: root is not a directory")
	ErrMissingTVDBKey   = errors.New("library: tvdb api key is required")
)

var (
	ErrEmptyQuery       = resolve.ErrEmptyQuery
	ErrTooManyTerms     = resolve.ErrTooManyTerms
	ErrConflictingTerms = resolve.ErrConflictingTerms
	ErrNoStrategyMatch  = resolve.ErrNoStrategyMatch
)
