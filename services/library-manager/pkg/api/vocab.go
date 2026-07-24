package api

import (
	"github.com/wyvernzora/kura/services/library-manager/internal/provider/tvdb"
	"github.com/wyvernzora/kura/services/library-manager/internal/reconcile"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/paths"
	"github.com/wyvernzora/kura/services/library-manager/pkg/api/refs"
)

type ReconcileApplyJobResult = reconcile.ApplyResult

const (
	KuraDir        = paths.KuraDir
	SeriesFileName = paths.SeriesFileName
)

func ParseOrdering(value string) (string, error) {
	return tvdb.ParseOrdering(value)
}

func SeriesDir(libRoot string, ref refs.Series) string {
	return paths.SeriesDir(libRoot, ref)
}
