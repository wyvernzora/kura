//go:build unix

package main

import (
	"syscall"
	"testing"

	"github.com/wyvernzora/kura/services/library/internal/fsop"
)

func TestConfigureUmaskAppliesProcessUmask(t *testing.T) {
	old := syscall.Umask(0)
	syscall.Umask(old)
	t.Cleanup(func() {
		syscall.Umask(old)
		fsop.SetPermissionMask(old)
	})

	err := configureUmask(func(key string) string {
		if key == envUmask {
			return "0077"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("configureUmask: %v", err)
	}

	got := syscall.Umask(old)
	if got != 0o077 {
		t.Fatalf("umask = %#o, want %#o", got, 0o077)
	}
}
