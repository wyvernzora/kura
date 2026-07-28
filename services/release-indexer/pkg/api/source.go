package api

// Source identifiers. These are the values RawPost.Source carries on the wire,
// which is why they live with the DTOs rather than in a crawler package: the
// service and its tests name a source without importing the crawler that
// produces it.
const (
	SourceDMHY = "dmhy"
	SourceNyaa = "nyaa"
)

var knownSources = []string{SourceDMHY, SourceNyaa}

// Sources returns the canonical registry of source identifiers.
//
// Adding a source also requires registering its runtime configuration and
// scheduler wiring in the release-indexer binary.
func Sources() []string {
	return append([]string(nil), knownSources...)
}
