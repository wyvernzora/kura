package refs

import "strings"

// Metadata identifies a series in an external metadata system.
type Metadata string

func ParseMetadata(value string) (Metadata, error) {
	ref := Metadata(strings.TrimSpace(value))
	if ref.Provider() == "" || ref.ID() == "" {
		return "", invalid("metadata ref", value, "expected <provider>:<id>")
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

// Source returns the provider component.
func (ref Metadata) Source() string {
	return ref.Provider()
}

// Value returns the provider-local identifier.
func (ref Metadata) Value() string {
	return ref.ID()
}
