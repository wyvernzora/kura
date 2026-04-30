package series

import "github.com/wyvernzora/kura/internal/refs"

type Handle struct {
	lib *Library
	ref refs.Series
}

func (h Handle) Ref() refs.Series {
	return h.ref
}

func (h Handle) Load() (Series, error) {
	return h.lib.repo.load(h.ref)
}
