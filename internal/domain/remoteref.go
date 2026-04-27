package domain

import (
	"fmt"
	"strings"
	"unicode"
)

// RemoteSeriesRef identifies a series in an external metadata system.
//
// It renders as "<source>:<id>", for example "tvdb:370070" or
// "imdb:tt10885406".
type RemoteSeriesRef struct {
	source string
	id     string
}

func ParseRemoteSeriesRef(value string) (RemoteSeriesRef, error) {
	value = strings.TrimSpace(value)
	source, id, ok := strings.Cut(value, ":")
	source = strings.ToLower(strings.TrimSpace(source))
	id = strings.TrimSpace(id)
	if !ok || source == "" || id == "" {
		return RemoteSeriesRef{}, fmt.Errorf("invalid remote series ref %q; expected <source>:<id>", value)
	}
	if !validRemoteRefSource(source) {
		return RemoteSeriesRef{}, fmt.Errorf("invalid remote series ref source %q", source)
	}
	if !validRemoteRefID(id) {
		return RemoteSeriesRef{}, fmt.Errorf("invalid remote series ref id %q", id)
	}
	return RemoteSeriesRef{source: source, id: id}, nil
}

func (r RemoteSeriesRef) Source() string {
	return r.source
}

func (r RemoteSeriesRef) ID() string {
	return r.id
}

func (r RemoteSeriesRef) String() string {
	if r.source == "" || r.id == "" {
		return ""
	}
	return r.source + ":" + r.id
}

func (r RemoteSeriesRef) Equal(other RemoteSeriesRef) bool {
	return r.source == other.source && r.id == other.id
}

func (r RemoteSeriesRef) IsZero() bool {
	return r.source == "" && r.id == ""
}

func (r RemoteSeriesRef) MarshalText() ([]byte, error) {
	return []byte(r.String()), nil
}

func (r *RemoteSeriesRef) UnmarshalText(data []byte) error {
	parsed, err := ParseRemoteSeriesRef(string(data))
	if err != nil {
		return err
	}
	*r = parsed
	return nil
}

func validRemoteRefSource(source string) bool {
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

func validRemoteRefID(id string) bool {
	return !strings.ContainsFunc(id, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || r == ':'
	})
}
