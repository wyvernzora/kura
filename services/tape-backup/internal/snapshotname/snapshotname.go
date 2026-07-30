// Package snapshotname owns the permanent on-tape name for one series
// generation.
package snapshotname

import (
	"encoding/base32"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxBytes = 255

var encoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// Format returns the canonical on-tape name for one series generation.
func Format(metadataRef string, generation int) (string, error) {
	if err := validateMetadataRef(metadataRef); err != nil {
		return "", err
	}
	if generation < 1 {
		return "", errors.New("generation must be at least 1")
	}
	name := encoding.EncodeToString([]byte(metadataRef)) + ".g" + strconv.Itoa(generation)
	if len(name) > maxBytes {
		return "", fmt.Errorf("snapshot name exceeds %d-byte limit", maxBytes)
	}
	return name, nil
}

// Parse decodes one canonical on-tape snapshot name.
func Parse(name string) (metadataRef string, generation int, err error) {
	if len(name) > maxBytes {
		return "", 0, fmt.Errorf("snapshot name exceeds %d-byte limit", maxBytes)
	}
	separator := strings.LastIndexByte(name, '.')
	if separator < 1 || separator == len(name)-1 || name[separator+1] != 'g' {
		return "", 0, fmt.Errorf(
			"snapshot name %q is missing .g<generation>",
			name,
		)
	}

	encodedRef := name[:separator]
	generationText := name[separator+2:]
	parsedGeneration, err := strconv.ParseUint(generationText, 10, strconv.IntSize)
	if err != nil || strconv.FormatUint(parsedGeneration, 10) != generationText {
		return "", 0, fmt.Errorf(
			"snapshot name %q has invalid generation %q",
			name,
			generationText,
		)
	}
	generation = int(parsedGeneration)
	if generation < 1 {
		return "", 0, fmt.Errorf(
			"snapshot name %q generation must be at least 1",
			name,
		)
	}

	decodedRef, err := encoding.DecodeString(encodedRef)
	if err != nil {
		return "", 0, fmt.Errorf(
			"snapshot name %q has invalid base32 metadataRef: %w",
			name,
			err,
		)
	}
	metadataRef = string(decodedRef)
	if err := validateMetadataRef(metadataRef); err != nil {
		return "", 0, err
	}
	if encoding.EncodeToString(decodedRef) != encodedRef {
		return "", 0, fmt.Errorf("snapshot name %q is not canonical", name)
	}
	return metadataRef, generation, nil
}

func validateMetadataRef(metadataRef string) error {
	if metadataRef == "" {
		return errors.New("metadataRef is required")
	}
	if !utf8.ValidString(metadataRef) {
		return errors.New("metadataRef must be valid UTF-8")
	}
	return nil
}
