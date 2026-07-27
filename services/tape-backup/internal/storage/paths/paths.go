// Package paths owns canonical path construction for tape-backup state.
package paths

import "path/filepath"

const (
	VolumeCatalogDirName         = "volumes"
	ActiveVolumeCatalogDirName   = "active"
	DetachedVolumeCatalogDirName = "detached"
)

// ActiveVolumeCatalogDir returns <state root>/volumes/active/.
func ActiveVolumeCatalogDir(stateRoot string) string {
	return filepath.Join(stateRoot, VolumeCatalogDirName, ActiveVolumeCatalogDirName)
}

// DetachedVolumeCatalogDir returns <state root>/volumes/detached/.
func DetachedVolumeCatalogDir(stateRoot string) string {
	return filepath.Join(stateRoot, VolumeCatalogDirName, DetachedVolumeCatalogDirName)
}
