package kura

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/renameio/v2"
	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/store"
)

func backupBefore(seriesDir string, name string) error {
	var source string
	switch name {
	case "series":
		source = store.SeriesMetadataPath(seriesDir)
	case "staged":
		source = store.StagedPath(seriesDir)
	case "trash":
		source = store.TrashPath(seriesDir)
	default:
		return fmt.Errorf("kura: unsupported history document %q", name)
	}
	data, err := os.ReadFile(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	historyDir := filepath.Join(seriesDir, fsroot.KuraDir, "history")
	if err := os.MkdirAll(historyDir, 0o755); err != nil {
		return err
	}
	target := filepath.Join(historyDir, fmt.Sprintf("%s.%s.json", name, time.Now().UTC().Format(time.RFC3339)))
	return renameio.WriteFile(target, data, 0o644)
}
