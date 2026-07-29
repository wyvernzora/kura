package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadTokenResolution(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		result, err := Load(Options{
			Disabled:  true,
			Token:     "ignored",
			TokenPath: filepath.Join(t.TempDir(), "token"),
		})
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if !result.Disabled || result.Token != "" || result.Source != "config-disabled" {
			t.Fatalf("Load() = %+v", result)
		}
	})

	t.Run("environment", func(t *testing.T) {
		result, err := Load(Options{
			Token:     "  secret\n",
			TokenPath: filepath.Join(t.TempDir(), "token"),
		})
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if result.Token != "secret" || result.Source != "environment" {
			t.Fatalf("Load() = %+v", result)
		}
	})

	t.Run("file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "token")
		if err := os.WriteFile(path, []byte("persisted\n"), 0o600); err != nil {
			t.Fatalf("write token: %v", err)
		}
		result, err := Load(Options{TokenPath: path})
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if result.Token != "persisted" || result.Source != "file" {
			t.Fatalf("Load() = %+v", result)
		}
	})

	t.Run("generated", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "token")
		result, err := Load(Options{TokenPath: path})
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if !result.Generated || len(result.Token) != 64 {
			t.Fatalf("Load() = %+v", result)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat token: %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("token mode = %o, want 600", info.Mode().Perm())
		}
	})
}

func TestLoadRejectsEmptyTokenFileExactly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	_, err := Load(Options{TokenPath: path})
	want := "auth: token file " + path + " exists but is empty (delete it to regenerate)"
	if err == nil || err.Error() != want {
		t.Fatalf("Load() error = %v, want %q", err, want)
	}
}
