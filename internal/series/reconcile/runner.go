package reconcile

import (
	"time"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/series/layout"
	"github.com/wyvernzora/kura/internal/series/state"
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
	return h.repo().load(h.ref)
}

func (h Runner) repo() repo {
	return repo{root: h.root()}
}

func (h Runner) files() files {
	return files{root: h.root()}
}

type seriesState = state.State

type episodeState = state.Episode

type MediaRecord = state.MediaRecord

type CompanionRecord = state.CompanionRecord

type repo struct {
	root string
}

func (r repo) load(ref refs.Series) (seriesState, error) {
	return state.NewRepository(r.root).Load(ref)
}

func (r repo) save(ref refs.Series, model seriesState) error {
	return state.NewRepository(r.root).Save(ref, model)
}

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

type editor struct {
	series *seriesState
}

func (e editor) promoteStaged(ref refs.Episode) (*MediaRecord, error) {
	return state.Editor{Series: e.series}.PromoteStaged(ref)
}
