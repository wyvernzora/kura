//go:build unix

package coord

import (
	"errors"
	"os"
	"syscall"
)

// processIsDefinitelyDead returns true iff signal(0, pid) returns
// ESRCH on the local host. Any other outcome (success, EPERM, other
// errno) is treated as "alive" — see IsStaleHolder for rationale.
func processIsDefinitelyDead(pid int) bool {
	if pid <= 0 {
		return true
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return errors.Is(err, syscall.ESRCH)
	}
	return false
}
