// Package volume owns shared archive-volume identity vocabulary.
package volume

import (
	"errors"
	"fmt"

	"github.com/oklog/ulid/v2"
)

// ID is the canonical uppercase ULID identifying archived content.
type ID string

// ParseID validates and returns a canonical archive volume ID.
func ParseID(value string) (ID, error) {
	if value == "" {
		return "", errors.New("volumeID is required")
	}
	parsed, err := ulid.ParseStrict(value)
	if err != nil || parsed.String() != value {
		return "", fmt.Errorf(
			"volumeID %q must be a 26-character uppercase Crockford base32 ULID",
			value,
		)
	}
	return ID(value), nil
}
