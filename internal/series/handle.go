package series

import (
	"errors"
	"os"
	"time"

	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/refs"
)

type Dependencies interface {
	LibraryRoot() fsroot.LibraryRoot
	MetadataSource() metadata.Source
	MediaInspector() Inspector
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

func (h Handle) Load() (Series, error) {
	return h.repo().load(h.ref)
}

func (h Handle) root() fsroot.LibraryRoot {
	return h.deps.LibraryRoot()
}

func (h Handle) source() metadata.Source {
	return h.deps.MetadataSource()
}

func (h Handle) inspector() Inspector {
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
