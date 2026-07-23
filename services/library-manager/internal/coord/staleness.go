package coord

// IsStaleHolder reports whether h is definitively dead and may be
// safely overwritten. Returns true only when:
//
//   - h.Host matches our hostname (we never auto-break cross-host
//     claims; the holding machine may be partitioned, not crashed),
//     AND
//   - signal(0) to h.PID returns ESRCH (process not found).
//
// Returns false on EPERM, success, ambiguous errors, or any
// cross-host claim. Errs on the side of "alive": false negatives
// just surface a BusyError the user can override with `kura
// reconcile recover --force`; false positives would let a peer
// clobber a live writer's state.
func IsStaleHolder(h Holder) bool {
	if h.Host != currentHost() {
		return false
	}
	return processIsDefinitelyDead(h.PID)
}
