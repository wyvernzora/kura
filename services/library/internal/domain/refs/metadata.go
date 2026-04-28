package refs

import (
	"fmt"
	"strings"
)

// Metadata identifies a series in an external metadata system.
type Metadata string

func ParseMetadata(value string) (Metadata, error) {
	ref := Metadata(strings.TrimSpace(value))
	if ref.Provider() == "" || ref.ID() == "" {
		return "", fmt.Errorf("invalid metadata ref %q; expected <provider>:<id>", value)
	}
	return ref, nil
}

func (ref Metadata) Provider() string {
	provider, _, ok := strings.Cut(strings.TrimSpace(string(ref)), ":")
	if !ok {
		return ""
	}
	return strings.TrimSpace(provider)
}

func (ref Metadata) ID() string {
	_, id, ok := strings.Cut(strings.TrimSpace(string(ref)), ":")
	if !ok {
		return ""
	}
	return strings.TrimSpace(id)
}

func (ref Metadata) String() string {
	return string(ref)
}
