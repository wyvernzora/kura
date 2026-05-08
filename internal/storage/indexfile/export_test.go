package indexfile

import "time"

// ProbeBaselineForTest returns the watcher's cached (hash, mtime, size).
// Test-only: lets external tests assert that synchronous mutation paths
// (e.g. SaveAndAdopt) keep the probe baseline aligned with the on-disk
// file so the next probe tick does not fire a no-op fullRefresh.
func (i *Index) ProbeBaselineForTest() (hash string, mtime time.Time, size int64) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.cachedHash, i.cachedMTime, i.cachedSize
}
