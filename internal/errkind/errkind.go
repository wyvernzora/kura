// Package errkind hosts the surface-shared error taxonomy: a stable
// Kind string per typed error, a Category that maps to JSON-RPC code
// severity, and the Coded interface surfaces use to render structured
// error payloads.
//
// Workflow / coord / jobs error types implement Coded by adding
// matching Kind, Category, and Data methods. Surfaces (MCP today,
// REST later) consume the interface; they do not need a switch over
// concrete error types beyond the Coded check.
package errkind

// Category labels how a typed error maps onto JSON-RPC numeric codes.
// Surfaces translate:
//
//	CategoryInvalidParams → -32602
//	CategoryInternalError → -32603
//	CategoryCancelled     → -32800
const (
	CategoryInvalidParams = "invalid_params"
	CategoryInternalError = "internal_error"
	CategoryCancelled     = "cancelled"
)

// Kind is the closed enum of MCP `data.kind` strings. Adding a new
// Kind requires a coordinated change in clients; new typed errors
// should reuse an existing Kind unless none fit.
const (
	KindNotFound            = "not_found"
	KindConflict            = "conflict"
	KindInvalidEpisode      = "invalid_episode"
	KindNoStaged            = "no_staged"
	KindPlanExpired         = "plan_expired"
	KindPlanApplied         = "plan_applied"
	KindStaleSnapshot       = "stale_snapshot"
	KindUnsupportedProvider = "unsupported_provider"
	KindInvalidRef          = "invalid_ref"
	KindProviderUnavailable = "provider_unavailable"
	KindBusy                = "busy"
	KindClaimStolen         = "claim_stolen"
	KindInternal            = "internal"
)

// Coded is the interface every typed error that wants to be rendered
// as a structured surface error implements. Surfaces walk the error
// chain via errors.As(err, &Coded) and use Kind / Category / Data to
// build the response.
type Coded interface {
	Kind() string
	Category() string
	Data() map[string]any
}
