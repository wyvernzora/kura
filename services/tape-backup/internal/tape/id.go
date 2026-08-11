// Package tape owns shared physical-cartridge vocabulary.
package tape

import (
	"errors"
	"fmt"
)

// ID is the eight-character LTO Ultrium barcode printed on a cartridge.
type ID string

// ParseID validates and returns an LTO Ultrium barcode.
func ParseID(value string) (ID, error) {
	id := ID(value)
	if err := validateID(id); err != nil {
		return "", err
	}
	return id, nil
}

// MediaGeneration reports the cartridge generation encoded in the barcode's
// media identifier.
//
// Like snapshot directories and obsolete ratios, generation is derived rather
// than stored. Capacity arithmetic uses filesystem-observed capacity and free
// space rather than values inferred from the generation.
func (id ID) MediaGeneration() string {
	if validateID(id) != nil {
		return ""
	}
	return mediaGenerationForIdentifier(string(id)[6:])
}

func validateID(id ID) error {
	value := string(id)
	if value == "" {
		return errors.New("tapeID is required")
	}
	if len(value) != 8 {
		return fmt.Errorf("tapeID %q must be exactly 8 characters", value)
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' {
			return fmt.Errorf("tapeID %q must use uppercase letters", value)
		}
	}
	for _, char := range value[:6] {
		if char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' {
			continue
		}
		return fmt.Errorf(
			"tapeID %q volume serial must contain only A-Z and 0-9",
			value,
		)
	}
	mediaIdentifier := value[6:]
	if mediaIdentifier == "CU" {
		return fmt.Errorf("tapeID %q identifies a cleaning cartridge", value)
	}
	if isWORMMediaIdentifier(mediaIdentifier) {
		return fmt.Errorf("tapeID %q identifies WORM media", value)
	}
	if mediaGenerationForIdentifier(mediaIdentifier) == "" {
		return fmt.Errorf(
			"tapeID %q has unsupported media identifier %q",
			value,
			mediaIdentifier,
		)
	}
	return nil
}

func isWORMMediaIdentifier(identifier string) bool {
	return len(identifier) == 2 &&
		identifier[0] == 'L' &&
		identifier[1] >= 'T' &&
		identifier[1] <= 'Z'
}

// Generation characters continue into letters (A = 10), so future
// generations require an explicit table entry rather than arithmetic.
var mediaGenerationByIdentifier = map[string]string{
	"L1": "LTO-1",
	"L2": "LTO-2",
	"L3": "LTO-3",
	"L4": "LTO-4",
	"L5": "LTO-5",
	"L6": "LTO-6",
	"L7": "LTO-7",
	"L8": "LTO-8",
	"L9": "LTO-9",
	"LA": "LTO-10",
	"M8": "LTO-7 Type M",
}

func mediaGenerationForIdentifier(identifier string) string {
	return mediaGenerationByIdentifier[identifier]
}
