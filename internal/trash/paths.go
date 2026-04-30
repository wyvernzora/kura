package trash

import (
	"path/filepath"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/refs"
)

const MetaFileName = "meta.json"
const DirName = "trash"

func EventDir(root string, ref refs.Series, id ulid.ULID) string {
	return filepath.Join(root, filepath.FromSlash(ref.String()), ".kura", DirName, id.String())
}

func MetaPath(root string, ref refs.Series, id ulid.ULID) string {
	return filepath.Join(EventDir(root, ref, id), MetaFileName)
}

func MediaPath(root string, ref refs.Series, id ulid.ULID, basename string) string {
	return filepath.Join(EventDir(root, ref, id), basename)
}
