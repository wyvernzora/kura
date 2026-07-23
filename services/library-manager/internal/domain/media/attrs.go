package media

import (
	"fmt"
	"regexp"
	"unicode"
	"unicode/utf8"
)

const (
	MaxAttrs      = 16
	MaxAttrKeyLen = 64
	MaxAttrValLen = 1024
)

var attrKeyRE = regexp.MustCompile(`^[a-z0-9_.]+$`)

// Attrs are schemaless, writer-owned breadcrumbs attached to a media record.
type Attrs map[string]string

// CloneAttrs returns a deep copy of attrs. Empty maps collapse to nil so JSON
// omitempty keeps absent and empty equivalent on the wire.
func CloneAttrs(attrs Attrs) Attrs {
	if len(attrs) == 0 {
		return nil
	}
	out := make(Attrs, len(attrs))
	for k, v := range attrs {
		out[k] = v
	}
	return out
}

// ValidateAttrs enforces Kura's shape limits without assigning meaning to keys.
func ValidateAttrs(attrs Attrs) error {
	if len(attrs) > MaxAttrs {
		return fmt.Errorf("attrs has %d entries, max %d", len(attrs), MaxAttrs)
	}
	for key, value := range attrs {
		if key == "" {
			return fmt.Errorf("attr key is empty")
		}
		if len(key) > MaxAttrKeyLen {
			return fmt.Errorf("attr key %q is %d bytes, max %d", key, len(key), MaxAttrKeyLen)
		}
		if !attrKeyRE.MatchString(key) {
			return fmt.Errorf("attr key %q must match [a-z0-9_.]+", key)
		}
		if !utf8.ValidString(value) {
			return fmt.Errorf("attr %q value is not valid UTF-8", key)
		}
		valueLen := utf8.RuneCountInString(value)
		if valueLen > MaxAttrValLen {
			return fmt.Errorf("attr %q value is %d chars, max %d", key, valueLen, MaxAttrValLen)
		}
		for _, r := range value {
			if unicode.IsControl(r) {
				return fmt.Errorf("attr %q value contains control character", key)
			}
		}
	}
	return nil
}
