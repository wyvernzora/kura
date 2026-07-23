package response

// UserAliasList is the response shape for the series-aliases endpoints
// (`GET / POST / DELETE /api/v1/series/{ref}/aliases`). Carries the
// persisted user aliases for the addressed series. TVDB-derived
// aliases never appear here — they're folded into searchKey at scan
// time and discarded.
type UserAliasList struct {
	Aliases []string `json:"aliases"`
}

// UserAliasMutation is the request body for POST + DELETE on the aliases
// endpoint. Empty / whitespace-only entries are dropped server-side;
// duplicates collapse into a single change. Unknown aliases on
// DELETE are no-ops.
type UserAliasMutation struct {
	Aliases []string `json:"aliases"`
}
