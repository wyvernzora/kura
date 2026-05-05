// Package auth provides the bearer-token deploy gate kura serve uses
// to control access to its REST + MCP-HTTP transports.
//
// This is NOT user-identity auth. Single shared secret, deploy-time
// access only. User-tier concerns (OIDC, scopes, federation) remain
// proxy responsibility per scratch/Product.md.
//
// Token resolution order, top-down (first match wins):
//
//  1. KURA_DISABLE_TOKEN=1|true  → auth bypassed entirely.
//  2. KURA_TOKEN=<value>         → use literal value as bearer token.
//  3. /var/lib/kura/token exists → read first non-empty line, trim.
//  4. (default)                   → generate 32-byte random hex,
//     persist to /var/lib/kura/token
//     with mode 0600.
//
// Operators fronting kura with an authenticating proxy use (1) to
// disable the gate. Operators who want a stable token across
// container restarts mount a file at /var/lib/kura/token. Everyone
// else gets a fresh token on first start that survives restart via
// the persisted file.
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

// EnvDisable is the env var name that, when "1" or "true", bypasses
// the token gate entirely. Use only when fronting kura with an
// authenticating proxy that handles user identity.
const EnvDisable = "KURA_DISABLE_TOKEN"

// EnvLiteral is the env var name that holds a literal bearer token
// value. Skips the file step entirely; takes precedence over the
// persisted file but not over EnvDisable.
const EnvLiteral = "KURA_TOKEN"

// tokenBytes is the entropy of a generated token in bytes. Hex-
// encoded to 64 ASCII chars on disk + on the wire.
const tokenBytes = 32

// generateMu serializes Load calls so a generate-and-persist race
// between two parallel callers (e.g. REST + MCP-HTTP transports
// inside one kura serve) yields exactly one persisted token rather
// than two competing writes.
var generateMu sync.Mutex

// Result captures the outcome of token resolution. Disabled=true
// means EnvDisable was set; callers should bypass the auth
// middleware. Token is the active bearer secret when Disabled=false.
// Generated=true marks the case where Load wrote a fresh token to
// the disk; callers can use this to print a copy-paste hint at boot.
type Result struct {
	Token     string
	Disabled  bool
	Generated bool
	Source    string // "env-disable" | "env-literal" | "file" | "generated"
}

// Load resolves the active bearer token per the package-level order.
// getenv defaults to os.Getenv when nil. tokenPath defaults to
// DefaultTokenPath when empty.
func Load(getenv func(string) string, tokenPath string) (Result, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	if tokenPath == "" {
		tokenPath = DefaultTokenPath
	}

	if isTrue(getenv(EnvDisable)) {
		return Result{Disabled: true, Source: "env-disable"}, nil
	}
	if v := strings.TrimSpace(getenv(EnvLiteral)); v != "" {
		return Result{Token: v, Source: "env-literal"}, nil
	}

	generateMu.Lock()
	defer generateMu.Unlock()

	// Re-check the file under the lock so two parallel Load calls
	// don't both decide to generate.
	if buf, err := os.ReadFile(tokenPath); err == nil {
		token := strings.TrimSpace(string(buf))
		if token == "" {
			return Result{}, fmt.Errorf("auth: token file %s exists but is empty (delete it to regenerate)", tokenPath)
		}
		return Result{Token: token, Source: "file"}, nil
	} else if !os.IsNotExist(err) {
		return Result{}, fmt.Errorf("auth: read token file %s: %w", tokenPath, err)
	}

	// Generate.
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return Result{}, fmt.Errorf("auth: generate token: %w", err)
	}
	token := hex.EncodeToString(buf)

	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o700); err != nil {
		return Result{}, fmt.Errorf("auth: mkdir %s: %w", filepath.Dir(tokenPath), err)
	}
	if err := os.WriteFile(tokenPath, []byte(token+"\n"), 0o600); err != nil {
		return Result{}, fmt.Errorf("auth: write token file %s: %w", tokenPath, err)
	}
	return Result{Token: token, Generated: true, Source: "generated"}, nil
}

// isTrue treats the kura-conventional truthy spellings as true.
// Matches Go's strconv.ParseBool plus the operator-friendly "TRUE".
func isTrue(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
