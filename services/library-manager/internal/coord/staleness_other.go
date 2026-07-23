//go:build !unix

package coord

// processIsDefinitelyDead is a stub for non-Unix platforms. We never
// claim a process is dead on Windows / Plan9 / etc. Effect: claims
// from the local host won't auto-break; user must run `kura reconcile
// recover --force` manually. Acceptable for v1 since Kura targets
// Linux/macOS.
func processIsDefinitelyDead(_ int) bool {
	return false
}
