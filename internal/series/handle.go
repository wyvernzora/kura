package series

import (
	"errors"
	"os"
	"time"

	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/metadata"
)

type Dependencies interface {
	LibraryRoot() string
	MetadataSource() metadata.Source
	MediaInspector() media.Inspector
	Now() time.Time
}

type Handle struct {
	deps Dependencies
	ref  refs.Series
}

func NewHandle(deps Dependencies, ref refs.Series) (Handle, error) {
	handle := Handle{deps: deps, ref: ref}
	if _, err := handle.files().seriesDir(ref); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Handle{}, SeriesNotFoundError{Ref: ref}
		}
		return Handle{}, err
	}
	if _, err := handle.repo().load(ref); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Handle{}, SeriesNotTrackedError{Ref: ref}
		}
		return Handle{}, err
	}
	return handle, nil
}

func (h Handle) Ref() refs.Series {
	return h.ref
}

func (h Handle) load() (seriesState, error) {
	return h.repo().load(h.ref)
}

func (h Handle) root() string {
	return h.deps.LibraryRoot()
}

func (h Handle) source() metadata.Source {
	return h.deps.MetadataSource()
}

func (h Handle) inspector() media.Inspector {
	return h.deps.MediaInspector()
}

func (h Handle) now() time.Time {
	return h.deps.Now()
}

func (h Handle) repo() repo {
	return repo{root: h.root()}
}

func (h Handle) files() files {
	return files{root: h.root()}
}
