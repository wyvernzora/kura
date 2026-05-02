package coord

import (
	"os"
	"time"
)

// hostnameOverride lets tests pin os.Hostname() output. Set to "" for
// production behavior.
var hostnameOverride = ""

// nowFunc lets tests pin time.Now(). Set via SetClock during tests;
// production code uses time.Now directly.
var nowFunc = time.Now

// currentHost returns the stable identity Kura stamps into Holder.Host
// and Mutator.Host. Resolution order:
//
//  1. hostnameOverride (test seam).
//  2. KURA_HOST_ID env var. Deployers set this to a stable identity
//     that survives container restarts so a previous container's
//     claim does not appear as "alive on a different host" to
//     IsStaleHolder. Without it, ephemeral container hostnames (Docker
//     default = container ID) make every restart-mid-apply require a
//     manual `kura reconcile recover`.
//  3. os.Hostname().
//  4. "unknown" if hostname lookup fails.
func currentHost() string {
	if hostnameOverride != "" {
		return hostnameOverride
	}
	if v := os.Getenv("KURA_HOST_ID"); v != "" {
		return v
	}
	host, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return host
}

// NewHolder captures pid/host/now/token for a claim acquisition.
// Started is truncated to second precision so the in-memory value
// round-trips through the RFC3339 wire format without loss.
func NewHolder(op, token string) Holder {
	return Holder{
		Op:      op,
		Token:   token,
		PID:     os.Getpid(),
		Host:    currentHost(),
		Started: nowFunc().UTC().Truncate(time.Second),
	}
}

// NewMutator captures pid/host/now for a successful CAS write stamp.
// At is truncated to second precision; same rationale as NewHolder.
func NewMutator(op string) Mutator {
	return Mutator{
		Op:   op,
		PID:  os.Getpid(),
		Host: currentHost(),
		At:   nowFunc().UTC().Truncate(time.Second),
	}
}
