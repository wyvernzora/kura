// Package encryptionkey loads and identifies the tape encryption key without
// exposing key material in errors.
package encryptionkey

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
)

const (
	encodedLength = 64
	rawLength     = 32
)

// Key is one AES-256 tape encryption key.
type Key [rawLength]byte

// Load reads a single lowercase-hex key line. One trailing newline is allowed.
func Load(path string) (Key, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Key{}, errors.New(
				"encryption key: configured key file does not exist",
			)
		}
		return Key{}, errors.New(
			"encryption key: configured key file cannot be read",
		)
	}
	if len(data) == encodedLength+1 && data[encodedLength] == '\n' {
		data = data[:encodedLength]
	}
	if len(data) != encodedLength || !lowerHex(data) {
		return Key{}, errors.New(
			"encryption key: configured key file must contain exactly 64 lowercase hexadecimal characters with one optional trailing newline",
		)
	}
	raw := make([]byte, rawLength)
	if _, err := hex.Decode(raw, data); err != nil {
		return Key{}, errors.New(
			"encryption key: configured key file must contain exactly 64 lowercase hexadecimal characters with one optional trailing newline",
		)
	}
	var key Key
	copy(key[:], raw)
	return key, nil
}

// Fingerprint returns the non-secret first eight bytes of SHA-256 in lowercase
// hexadecimal.
func (k Key) Fingerprint() string {
	sum := sha256.Sum256(k[:])
	return hex.EncodeToString(sum[:8])
}

func lowerHex(data []byte) bool {
	for _, value := range data {
		if value >= '0' && value <= '9' {
			continue
		}
		if value < 'a' || value > 'f' {
			return false
		}
	}
	return true
}
