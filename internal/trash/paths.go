package trash

import (
	"path/filepath"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/refs"
)

const MetaFileName = "meta.json"
const DirName = "trash"

func EventDir(root fsroot.LibraryRoot, ref refs.Series, id ulid.ULID) string {
	return filepath.Join(root.Join(ref.String()), ".kura", DirName, id.String())
}

func MetaPath(root fsroot.LibraryRoot, ref refs.Series, id ulid.ULID) string {
	return filepath.Join(EventDir(root, ref, id), MetaFileName)
}

func MediaPath(root fsroot.LibraryRoot, ref refs.Series, id ulid.ULID, basename string) string {
	return filepath.Join(EventDir(root, ref, id), basename)
}
