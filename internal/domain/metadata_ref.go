package domain

import "strings"

// MetadataRef identifies a series in an external metadata system.
type MetadataRef string

func (ref MetadataRef) Source() string {
	source, _, ok := strings.Cut(strings.TrimSpace(string(ref)), ":")
	if !ok {
		return ""
	}
	return strings.TrimSpace(source)
}

func (ref MetadataRef) Value() string {
	_, value, ok := strings.Cut(strings.TrimSpace(string(ref)), ":")
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func (ref MetadataRef) String() string {
	return string(ref)
}
