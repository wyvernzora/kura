// Package auth provides the bearer-token deploy gate kura serve uses
// to control access to its REST + MCP-HTTP transports.
//
// This is NOT user-identity auth. Single shared secret, deploy-time
// access only. User-tier concerns (OIDC, scopes, federation) remain
// proxy responsibility per scratch/Product.md.
//
// Token resolution order, top-down (first match wins):
//
//  1. Options.Disabled           → auth bypassed entirely.
//  2. Options.Token              → use literal value as bearer token.
//  3. Options.TokenPath exists   → read first non-empty line, trim.
//  4. (default)                  → generate 32-byte random hex,
//     persist to /var/lib/kura/token
//     with mode 0600.
//
// The command layer sources Disabled and TokenPath from TOML and Token
// from KURA_TOKEN. Operators fronting kura with an authenticating proxy
// disable the gate in TOML.
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

// DefaultTokenPath is the canonical location for the persisted
// bearer token. Container images should ensure this path's parent
// directory exists and is writable by the kura process.
const DefaultTokenPath = "/var/lib/kura/token"

// EnvLiteral is the env var name that holds a literal bearer token
// value. The command layer reads it and passes the value to Load.
const EnvLiteral = "KURA_TOKEN"

// tokenBytes is the entropy of a generated token in bytes. Hex-
// encoded to 64 ASCII chars on disk + on the wire.
const tokenBytes = 32

// generateMu serializes Load calls so a generate-and-persist race
// between two parallel callers (e.g. REST + MCP-HTTP transports
// inside one kura serve) yields exactly one persisted token rather
// than two competing writes.
var generateMu sync.Mutex

// Options contains already-resolved token settings.
type Options struct {
	Disabled  bool
	Token     string
	TokenPath string
}

// Result captures the outcome of token resolution. Disabled=true means
// callers should bypass the auth middleware. Token is the active bearer
// secret when Disabled=false.
// Generated=true marks the case where Load wrote a fresh token to
// the disk; callers can use this to print a copy-paste hint at boot.
type Result struct {
	Token     string
	Disabled  bool
	Generated bool
	Source    string // "config-disabled" | "environment" | "file" | "generated"
}

// Load resolves the active bearer token per the package-level order.
// TokenPath defaults to DefaultTokenPath when empty.
func Load(opts Options) (Result, error) {
	if opts.TokenPath == "" {
		opts.TokenPath = DefaultTokenPath
	}

	if opts.Disabled {
		return Result{Disabled: true, Source: "config-disabled"}, nil
	}
	if v := strings.TrimSpace(opts.Token); v != "" {
		return Result{Token: v, Source: "environment"}, nil
	}

	generateMu.Lock()
	defer generateMu.Unlock()

	// Re-check the file under the lock so two parallel Load calls
	// don't both decide to generate.
	if buf, err := os.ReadFile(opts.TokenPath); err == nil {
		token := strings.TrimSpace(string(buf))
		if token == "" {
			return Result{}, fmt.Errorf("auth: token file %s exists but is empty (delete it to regenerate)", opts.TokenPath)
		}
		return Result{Token: token, Source: "file"}, nil
	} else if !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("auth: read token file %s: %w", opts.TokenPath, err)
	}

	// Generate.
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return Result{}, fmt.Errorf("auth: generate token: %w", err)
	}
	token := hex.EncodeToString(buf)

	if err := os.MkdirAll(filepath.Dir(opts.TokenPath), 0o700); err != nil {
		return Result{}, fmt.Errorf("auth: mkdir %s: %w", filepath.Dir(opts.TokenPath), err)
	}
	if err := os.WriteFile(opts.TokenPath, []byte(token+"\n"), 0o600); err != nil {
		return Result{}, fmt.Errorf("auth: write token file %s: %w", opts.TokenPath, err)
	}
	return Result{Token: token, Generated: true, Source: "generated"}, nil
}
