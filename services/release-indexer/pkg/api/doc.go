// Package api is the release-indexer public wire contract: the request and
// response shapes served under /api/releases/v1.
//
// It is a leaf. It imports nothing from internal/ and holds no storage,
// dispatch, or network behaviour — only serialized DTOs and the enum values
// that appear in them. Handlers, the CLI, and contract tests may import it;
// the gateway deliberately does not (it declares its own decode structs, see
// the unified API plan §7.1).
//
// Field naming follows the suite convention: camelCase JSON, `Bytes` suffix on
// byte counts, `...At` on timestamps, `Id` suffix on identifiers, and explicit
// null for nullable database facts rather than a zero-value coercion.
package api

// Health is the GET /healthz body. The gateway's health fan-out calls this
// directly on each leaf to build its component tree, so the shape is public
// contract rather than an operational detail.
type Health struct {
	Ok      bool   `json:"ok"`
	Version string `json:"version"`
}
