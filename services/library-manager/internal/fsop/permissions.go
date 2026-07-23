package fsop

import (
	"io/fs"
	"sync/atomic"
)

var permissionMask atomic.Int64

func init() {
	permissionMask.Store(-1)
}

// SetPermissionMask records the process umask Kura configured at startup so
// moved media normalization can reduce permissions consistently. It returns a
// restore function for tests.
func SetPermissionMask(mask int) func() {
	old := permissionMask.Swap(int64(mask))
	return func() {
		permissionMask.Store(old)
	}
}

func movedFileMode(mode fs.FileMode) fs.FileMode {
	if mode&0o400 != 0 {
		mode |= 0o040
	}
	if mode&0o200 != 0 {
		mode |= 0o020
	}
	if mask := permissionMask.Load(); mask >= 0 {
		mode &^= fs.FileMode(mask)
	}
	return mode
}
