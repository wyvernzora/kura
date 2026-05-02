package coord

import (
	"errors"
	"os"
	"strconv"
)

// DefaultMaxAttempts caps short-op CAS retries: one initial attempt
// plus one silent retry on ConflictError. Matches the
// KURA_CONFLICT_RETRIES=1 default.
const DefaultMaxAttempts = 2

// AttemptsFromEnv reads KURA_CONFLICT_RETRIES and returns the total
// attempt count (initial + retries). Defaults to DefaultMaxAttempts
// when the env var is unset, malformed, or negative. Call sites that
// compose RetryOnConflict use this so the operator-tunable retry
// budget remains discoverable from one place.
func AttemptsFromEnv() int {
	v := os.Getenv("KURA_CONFLICT_RETRIES")
	if v == "" {
		return DefaultMaxAttempts
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return DefaultMaxAttempts
	}
	return n + 1
}

// RetryOnConflict invokes fn up to maxAttempts times. Returns nil on
// success, the last ConflictError on exhaustion, or any non-conflict
// error immediately. Used by the *Retry coordinator methods and by
// callers that need conflict retry without coordinator serialization
// (e.g. the index rebuild loop).
func RetryOnConflict(maxAttempts int, fn func() error) error {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}
		var conflict *ConflictError
		if !errors.As(err, &conflict) {
			return err
		}
		lastErr = err
	}
	return lastErr
}
