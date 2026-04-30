package series

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/refs"
)

type files struct {
	root fsroot.LibraryRoot
}

type fileFacts struct {
	Size  int64
	MTime time.Time
}

func (f files) seriesDir(ref refs.Series) (fsroot.SeriesDir, error) {
	return f.root.SeriesDir(ref.String())
}

func (f files) stat(path string) (fileFacts, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileFacts{}, err
	}
	if info.IsDir() {
		return fileFacts{}, fmt.Errorf("series: path %q is a directory", path)
	}
	return fileFacts{Size: info.Size(), MTime: info.ModTime().UTC()}, nil
}

func (f files) canonicalPath(ref refs.Series, episode refs.Episode, media MediaRecord) (string, error) {
	title := CleanFileTitle(ref.String())
	facts := MediaFilenameFacts{Source: ParseMediaSource(media.Source)}
	if media.Resolution != "" {
		if resolution, err := ParseResolution(media.Resolution); err == nil {
			facts.Resolution = resolution
		}
	}
	filename := BuildMediaFilename(title, episode, facts, filepath.Ext(media.Path)).String()
	if episode.Season() == 0 {
		return filename, nil
	}
	return filepath.ToSlash(filepath.Join(fmt.Sprintf("Season %d", episode.Season()), filename)), nil
}

func (f files) move(from, to string) error {
	return safeMoveFile(from, to)
}

func safeMoveFile(from string, to string) error {
	if from == to {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	if err := os.Rename(from, to); err == nil {
		return syncDir(filepath.Dir(to))
	} else if !isCrossDeviceMove(err) {
		return err
	}
	return copyThenRemove(from, to)
}

func copyThenRemove(from string, to string) error {
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("library: cannot move directory %q as file", from)
	}

	targetDir := filepath.Dir(to)
	tmp, err := os.CreateTemp(targetDir, "."+filepath.Base(to)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := io.Copy(tmp, source); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(info.Mode()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chtimes(tmpName, info.ModTime(), info.ModTime()); err != nil {
		return err
	}
	if err := os.Rename(tmpName, to); err != nil {
		return err
	}
	removeTmp = false
	if err := syncDir(targetDir); err != nil {
		return err
	}
	if err := os.Remove(from); err != nil {
		return err
	}
	return syncDir(filepath.Dir(from))
}

func isCrossDeviceMove(err error) bool {
	linkErr, ok := errors.AsType[*os.LinkError](err)
	if !ok {
		return false
	}
	return errors.Is(linkErr.Err, syscall.EXDEV)
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return nil
		}
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
