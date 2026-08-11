package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	// DefaultTokenPath is the suite token file location.
	DefaultTokenPath = "/var/lib/kura/token"
	// EnvLiteral supplies a literal bearer token without putting it in TOML.
	EnvLiteral = "KURA_TOKEN"
	tokenBytes = 32
)

var generateMu sync.Mutex

// Options contains already-resolved token settings.
type Options struct {
	Disabled  bool
	Token     string
	TokenPath string
}

// Result captures the selected authentication posture.
type Result struct {
	Token     string
	Disabled  bool
	Generated bool
	Source    string
}

// Load resolves the token from explicit disablement, the environment value,
// an existing file, or a newly generated persisted value, in that order.
func Load(opts Options) (Result, error) {
	if opts.TokenPath == "" {
		opts.TokenPath = DefaultTokenPath
	}
	if opts.Disabled {
		return Result{Disabled: true, Source: "config-disabled"}, nil
	}
	if token := strings.TrimSpace(opts.Token); token != "" {
		return Result{Token: token, Source: "environment"}, nil
	}

	generateMu.Lock()
	defer generateMu.Unlock()

	if data, err := os.ReadFile(opts.TokenPath); err == nil {
		token := strings.TrimSpace(string(data))
		if token == "" {
			return Result{}, fmt.Errorf(
				"auth: token file %s exists but is empty (delete it to regenerate)",
				opts.TokenPath,
			)
		}
		return Result{Token: token, Source: "file"}, nil
	} else if !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("auth: read token file %s: %w", opts.TokenPath, err)
	}

	data := make([]byte, tokenBytes)
	if _, err := rand.Read(data); err != nil {
		return Result{}, fmt.Errorf("auth: generate token: %w", err)
	}
	token := hex.EncodeToString(data)
	if err := os.MkdirAll(filepath.Dir(opts.TokenPath), 0o700); err != nil {
		return Result{}, fmt.Errorf("auth: mkdir %s: %w", filepath.Dir(opts.TokenPath), err)
	}
	if err := os.WriteFile(opts.TokenPath, []byte(token+"\n"), 0o600); err != nil {
		return Result{}, fmt.Errorf("auth: write token file %s: %w", opts.TokenPath, err)
	}
	return Result{
		Token:     token,
		Generated: true,
		Source:    "generated",
	}, nil
}
