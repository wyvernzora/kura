//go:build unix

package main

import (
	"syscall"
	"testing"

	"github.com/wyvernzora/kura/services/library-manager/internal/fsop"
)

func TestConfigureUmaskAppliesProcessUmask(t *testing.T) {
	old := syscall.Umask(0)
	syscall.Umask(old)
	t.Cleanup(func() {
		syscall.Umask(old)
		fsop.SetPermissionMask(old)
	})

	err := configureUmask("0077")
	if err != nil {
		t.Fatalf("configureUmask: %v", err)
	}

	got := syscall.Umask(old)
	if got != 0o077 {
		t.Fatalf("umask = %#o, want %#o", got, 0o077)
	}
}
