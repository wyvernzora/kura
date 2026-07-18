package series

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

const MaxTags = 64

var tagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9:_-]{0,63}$`)

// ValidateTag lowercases and checks one opaque series tag. Kura owns only
// this syntax; callers assign meaning to individual values.
func ValidateTag(tag string) error {
	tag = strings.ToLower(tag)
	if !tagPattern.MatchString(tag) {
		return fmt.Errorf("tag %q must match %s", tag, tagPattern)
	}
	return nil
}

// NormalizeTags lowercases, validates, deduplicates, and sorts a persisted tag set.
func NormalizeTags(tags []string) ([]string, error) {
	if len(tags) > MaxTags {
		return nil, fmt.Errorf("tag count %d exceeds limit %d", len(tags), MaxTags)
	}
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tag = strings.ToLower(tag)
		if err := ValidateTag(tag); err != nil {
			return nil, err
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	slices.Sort(out)
	return out, nil
}

// AddTag adds tag when absent. The caller must validate tag first.
func (s *Series) AddTag(tag string) bool {
	if slices.Contains(s.Tags, tag) {
		return false
	}
	s.Tags = append(s.Tags, tag)
	return true
}

// RemoveTag removes tag when present.
func (s *Series) RemoveTag(tag string) bool {
	idx := slices.Index(s.Tags, tag)
	if idx < 0 {
		return false
	}
	s.Tags = append(s.Tags[:idx], s.Tags[idx+1:]...)
	return true
}
