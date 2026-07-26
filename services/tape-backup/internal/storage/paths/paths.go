// Package paths owns canonical path construction for tape-backup state.
package paths

import "path/filepath"

const (
	VolumeCatalogDirName         = "volumes"
	ActiveVolumeCatalogDirName   = "active"
	DetachedVolumeCatalogDirName = "detached"
	VolumeCatalogExtension       = ".json"
)

// ActiveVolumeCatalogDir returns <state root>/volumes/active/.
func ActiveVolumeCatalogDir(stateRoot string) string {
	return filepath.Join(stateRoot, VolumeCatalogDirName, ActiveVolumeCatalogDirName)
}

// ActiveVolumeCatalog returns
// <state root>/volumes/active/<volumeID>.json.
func ActiveVolumeCatalog(stateRoot, volumeID string) string {
	return filepath.Join(
		ActiveVolumeCatalogDir(stateRoot),
		volumeID+VolumeCatalogExtension,
	)
}

// DetachedVolumeCatalogDir returns <state root>/volumes/detached/.
func DetachedVolumeCatalogDir(stateRoot string) string {
	return filepath.Join(stateRoot, VolumeCatalogDirName, DetachedVolumeCatalogDirName)
}

// DetachedVolumeCatalog returns
// <state root>/volumes/detached/<volumeID>.json.
func DetachedVolumeCatalog(stateRoot, volumeID string) string {
	return filepath.Join(
		DetachedVolumeCatalogDir(stateRoot),
		volumeID+VolumeCatalogExtension,
	)
}
