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

func currentHost() string {
	if hostnameOverride != "" {
		return hostnameOverride
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
