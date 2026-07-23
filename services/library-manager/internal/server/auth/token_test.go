package auth

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLoad_EnvDisable(t *testing.T) {
	cases := []string{"1", "true", "TRUE", "yes", "on"}
	for _, v := range cases {
		t.Run(v, func(t *testing.T) {
			env := envMap{EnvDisable: v}
			r, err := Load(env.Get, t.TempDir()+"/token")
			if err != nil {
				t.Fatal(err)
			}
			if !r.Disabled {
				t.Errorf("Disabled: got false want true")
			}
			if r.Token != "" {
				t.Errorf("Token: got %q want empty", r.Token)
			}
			if r.Source != "env-disable" {
				t.Errorf("Source: got %q", r.Source)
			}
		})
	}
}

func TestLoad_EnvLiteral(t *testing.T) {
	env := envMap{EnvLiteral: "secret-value"}
	r, err := Load(env.Get, t.TempDir()+"/token")
	if err != nil {
		t.Fatal(err)
	}
	if r.Disabled {
		t.Errorf("Disabled: unexpectedly true")
	}
	if r.Token != "secret-value" {
		t.Errorf("Token: got %q want secret-value", r.Token)
	}
	if r.Source != "env-literal" {
		t.Errorf("Source: got %q", r.Source)
	}
}

func TestLoad_EnvLiteral_TrimsWhitespace(t *testing.T) {
	env := envMap{EnvLiteral: "  padded\n"}
	r, err := Load(env.Get, t.TempDir()+"/token")
	if err != nil {
		t.Fatal(err)
	}
	if r.Token != "padded" {
		t.Errorf("Token: got %q want padded", r.Token)
	}
}

func TestLoad_DisableTakesPrecedence(t *testing.T) {
	env := envMap{EnvDisable: "1", EnvLiteral: "ignored"}
	r, err := Load(env.Get, t.TempDir()+"/token")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Disabled {
		t.Errorf("Disabled: got false want true")
	}
}

func TestLoad_ReadExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("abc123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := Load(envMap{}.Get, path)
	if err != nil {
		t.Fatal(err)
	}
	if r.Token != "abc123" {
		t.Errorf("Token: got %q want abc123", r.Token)
	}
	if r.Source != "file" {
		t.Errorf("Source: got %q", r.Source)
	}
	if r.Generated {
		t.Errorf("Generated: unexpectedly true")
	}
}

func TestLoad_EmptyFileErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("\n  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(envMap{}.Get, path)
	if err == nil {
		t.Fatal("expected error on empty file")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error: got %v want contains 'empty'", err)
	}
}

func TestLoad_GenerateAndPersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")

	r1, err := Load(envMap{}.Get, path)
	if err != nil {
		t.Fatal(err)
	}
	if !r1.Generated {
		t.Errorf("Generated: got false want true")
	}
	if len(r1.Token) != 64 {
		t.Errorf("Token length: got %d want 64", len(r1.Token))
	}

	// Subsequent Load reads the persisted file.
	r2, err := Load(envMap{}.Get, path)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Generated {
		t.Errorf("second Load Generated: got true")
	}
	if r2.Token != r1.Token {
		t.Errorf("token mismatch across calls: %q vs %q", r1.Token, r2.Token)
	}
	if r2.Source != "file" {
		t.Errorf("second Load Source: got %q want file", r2.Source)
	}
}

func TestLoad_GeneratedFileIs0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if _, err := Load(envMap{}.Get, path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("permissions: got %o want 0600", mode)
	}
}

func TestLoad_RaceSafe_SingleGeneratedToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")

	const N = 10
	var (
		wg      sync.WaitGroup
		results = make([]Result, N)
		errs    = make([]error, N)
	)
	for i := range N {
		wg.Go(func() {
			results[i], errs[i] = Load(envMap{}.Get, path)
		})
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("Load %d: %v", i, err)
		}
	}
	first := results[0].Token
	for i, r := range results {
		if r.Token != first {
			t.Errorf("token %d differs: %q vs %q", i, r.Token, first)
		}
	}
	// At most one Load should report Generated=true.
	generated := 0
	for _, r := range results {
		if r.Generated {
			generated++
		}
	}
	if generated != 1 {
		t.Errorf("Generated count: got %d want 1", generated)
	}
}

// envMap is a tiny stub for getenv. Cleaner than plumbing real env
// vars through tests.
type envMap map[string]string

func (e envMap) Get(k string) string { return e[k] }
