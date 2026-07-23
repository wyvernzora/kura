// Package rawpost holds the shared ingestion contract between source crawlers,
// in-process ingestion, and POST /ingest. It is a leaf and imports nothing
// internal.
package rawpost

import "time"

// SourceDMHY is the stable DMHY source identifier. It is inlined here (the leaf
// wire contract) so the takuhai service and its tests never import the foreign
// sources/dmhy crawler module just to name the source.
const SourceDMHY = "dmhy"

// SourceNyaa is the stable Nyaa source identifier.
const SourceNyaa = "nyaa"

var knownSources = []string{SourceDMHY, SourceNyaa}

// Sources returns the canonical registry of source identifiers known to the
// shared wire contract.
//
// Adding a source requires registering its runtime configuration and scheduler
// wiring in the release-indexer binary.
func Sources() []string {
	return append([]string(nil), knownSources...)
}

// RawPost is one crawled post. Source packages emit raw fields (title, magnet,
// metadata, size) and do not normalize the infohash; internal/ingest derives the
// canonical dedup key from Magnet. There is deliberately no infohash field here.
//
// Field names/types mirror store.IngestParams (the persistence input) so the P4
// /ingest mapping is a trivial field copy, but this leaf imports neither store
// nor the source layer.
type RawPost struct {
	Title       string    `json:"title"`        // raw, unparsed
	Magnet      string    `json:"magnet"`       // representative seed; takuhai derives the infohash from this
	Source      string    `json:"source"`       // e.g. "dmhy"
	SourceID    string    `json:"source_id"`    // source-native stable id (DMHY GUID) — unique with Source
	URL         string    `json:"url"`          //
	PublishedAt time.Time `json:"published_at"` //
	SizeBytes   int64     `json:"size_bytes"`   // parsed total size in bytes; 0 = unset
}

// QueueStats mirrors the small queue wake signal returned by /ingest.
type QueueStats struct {
	Available int64 `json:"available"`
	Leased    int64 `json:"leased"`
	Exhausted int64 `json:"exhausted"`
}

// IngestBatch is the per-call breakdown of one POST /ingest: how each post in the
// posted batch was resolved. skipped is owned by the REST ingest handler (magnet yields no
// canonical v1 btih — pure-v2/malformed); the other four come from the store seam
// (queued = first-seen infohash; recheck = new evidence on a known infohash;
// duplicate = same (source, source_id), nothing linked; conflict = reused
// (source, source_id) with a NEW infohash, rejected as an orphan-release no-op).
type IngestBatch struct {
	New       int `json:"new"`
	Updated   int `json:"updated"`
	Duplicate int `json:"duplicate"`
	Conflict  int `json:"conflict"`
	Skipped   int `json:"skipped"`
}

// IngestSummary is the POST /ingest response: the per-call Batch buckets PLUS the
// current durable Queue counts. n8n wakes the matcher off queue.available, not the
// Batch buckets, so the wake signal survives a lost response.
type IngestSummary struct {
	Batch IngestBatch `json:"batch"`
	Queue QueueStats  `json:"queue"`
}
