package metadata

import (
	"fmt"
	"strings"
	"unicode"
)

// MetadataRef identifies a series in an external metadata system.
//
// It renders as "<source>:<id>", for example "tvdb:370070" or
// "imdb:tt10885406".
type MetadataRef struct {
	source string
	id     string
}

func ParseMetadataRef(value string) (MetadataRef, error) {
	value = strings.TrimSpace(value)
	source, id, ok := strings.Cut(value, ":")
	source = strings.ToLower(strings.TrimSpace(source))
	id = strings.TrimSpace(id)
	if !ok || source == "" || id == "" {
		return MetadataRef{}, fmt.Errorf("invalid metadata ref %q; expected <source>:<id>", value)
	}
	if !validMetadataRefSource(source) {
		return MetadataRef{}, fmt.Errorf("invalid metadata ref source %q", source)
	}
	if !validMetadataRefID(id) {
		return MetadataRef{}, fmt.Errorf("invalid metadata ref id %q", id)
	}
	return MetadataRef{source: source, id: id}, nil
}

func (r MetadataRef) Source() string {
	return r.source
}

func (r MetadataRef) ID() string {
	return r.id
}

func (r MetadataRef) String() string {
	if r.source == "" || r.id == "" {
		return ""
	}
	return r.source + ":" + r.id
}

func (r MetadataRef) Equal(other MetadataRef) bool {
	return r.source == other.source && r.id == other.id
}

func (r MetadataRef) IsZero() bool {
	return r.source == "" && r.id == ""
}

func (r MetadataRef) MarshalText() ([]byte, error) {
	return []byte(r.String()), nil
}

func (r *MetadataRef) UnmarshalText(data []byte) error {
	parsed, err := ParseMetadataRef(string(data))
	if err != nil {
		return err
	}
	*r = parsed
	return nil
}

func validMetadataRefSource(source string) bool {
	for index, r := range source {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if index > 0 && (r == '-' || r == '_') {
			continue
		}
		return false
	}
	return true
}

func validMetadataRefID(id string) bool {
	return !strings.ContainsFunc(id, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || r == ':'
	})
}
