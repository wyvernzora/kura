package encryptionkey_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wyvernzora/kura/services/tape-backup/internal/encryptionkey"
)

func TestLoadAcceptsLowercaseHexWithOptionalTrailingNewline(t *testing.T) {
	const encoded = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	for _, suffix := range []string{"", "\n"} {
		t.Run("suffix_"+strings.ReplaceAll(suffix, "\n", "newline"), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "tape.key")
			if err := os.WriteFile(path, []byte(encoded+suffix), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			key, err := encryptionkey.Load(path)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			const wantFingerprint = "630dcd2966c43366"
			if got := key.Fingerprint(); got != wantFingerprint {
				t.Fatalf("Fingerprint() = %q, want %q", got, wantFingerprint)
			}
		})
	}
}

func TestLoadRejectsMissingAndMalformedWithoutEchoingContents(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		_, err := encryptionkey.Load(filepath.Join(t.TempDir(), "missing.key"))
		const want = "encryption key: configured key file does not exist"
		if err == nil || err.Error() != want {
			t.Fatalf("Load() error = %v, want %q", err, want)
		}
	})

	t.Run("malformed", func(t *testing.T) {
		const secret = "THIS-MUST-NEVER-APPEAR-IN-THE-ERROR"
		path := filepath.Join(t.TempDir(), "tape.key")
		if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		_, err := encryptionkey.Load(path)
		const want = "encryption key: configured key file must contain exactly 64 lowercase hexadecimal characters with one optional trailing newline"
		if err == nil || err.Error() != want {
			t.Fatalf("Load() error = %v, want %q", err, want)
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("Load() error echoed key file contents: %q", err)
		}
	})
}
