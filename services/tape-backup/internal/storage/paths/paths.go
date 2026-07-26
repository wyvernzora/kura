// Package paths owns canonical path construction for tape-backup state.
package paths

import "path/filepath"

const (
	TapeCatalogDirName   = "tapes"
	TapeCatalogExtension = ".json"
)

// TapeCatalogDir returns <state root>/tapes/.
func TapeCatalogDir(stateRoot string) string {
	return filepath.Join(stateRoot, TapeCatalogDirName)
}

// TapeCatalog returns <state root>/tapes/<tapeID>.json.
func TapeCatalog(stateRoot, tapeID string) string {
	return filepath.Join(TapeCatalogDir(stateRoot), tapeID+TapeCatalogExtension)
}
