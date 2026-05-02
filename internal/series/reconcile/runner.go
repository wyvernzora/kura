package reconcile

import (
	"time"

	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	domainseries "github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/series/layout"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
)

type Runner struct {
	rootPath string
	ref      refs.Series
	nowFunc  func() time.Time
}

func NewRunner(root string, ref refs.Series, now func() time.Time) Runner {
	return Runner{rootPath: root, ref: ref, nowFunc: now}
}

func (h Runner) root() string {
	return h.rootPath
}

func (h Runner) now() time.Time {
	return h.nowFunc()
}

func (h Runner) load() (seriesState, error) {
	model, err := seriesfile.Load(h.rootPath, h.ref)
	if err != nil {
		return seriesState{}, err
	}
	return *model, nil
}

func (h Runner) save(model seriesState) error {
	model.Ref = h.ref
	return seriesfile.Save(h.rootPath, &model)
}

func (h Runner) files() files {
	return files{root: h.root()}
}

type seriesState = domainseries.Series

type episodeState = domainseries.Episode

type MediaRecord = media.Record

type CompanionRecord = media.Companion

type files struct {
	root string
}

func (f files) seriesDir(ref refs.Series) (layout.SeriesDir, error) {
	return layout.NewFiles(f.root).SeriesDir(ref)
}

func (f files) canonicalPath(ref refs.Series, episode refs.Episode, record MediaRecord) (string, error) {
	return layout.NewFiles(f.root).CanonicalPath(ref, episode, record)
}

func (f files) move(from, to string) error {
	return layout.NewFiles(f.root).Move(from, to)
}
