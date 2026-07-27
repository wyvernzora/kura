package api

import "time"

// RawPost is one crawled posting, as accepted by POST /api/v1/releases/ingest
// and as emitted by the in-process source crawlers.
//
// There is deliberately no infohash field: the service derives the canonical
// dedup key from Magnet, so a producer cannot disagree with it about identity.
//
// This supersedes pkg/rawpost, which carried the same shape in snake_case. That
// package is folded into this one when the ingest handler moves to /api/v1
// (plan §8.2); until then both exist and only this one is public contract.
type RawPost struct {
	Title       string    `json:"title"`
	Magnet      string    `json:"magnet"`
	Source      string    `json:"source"`
	SourceID    string    `json:"sourceId"`
	URL         string    `json:"url"`
	PublishedAt time.Time `json:"publishedAt"`
	// SizeBytes is the parsed total size; 0 means unset, and the store maps it
	// to SQL NULL — which is why sizeBytes is nullable on the way back out.
	SizeBytes int64 `json:"sizeBytes"`
}

// IngestRequest is the POST /api/v1/releases/ingest body.
type IngestRequest struct {
	Posts []RawPost `json:"posts"`
}

// IngestBatch is the per-call breakdown of one ingest.
//
//   - New: first sighting of an infohash.
//   - Updated: new evidence on a known infohash.
//   - Duplicate: same (source, sourceId), nothing linked.
//   - Conflict: a reused (source, sourceId) carrying a different infohash,
//     rejected as an orphan-release no-op.
//   - Skipped: the magnet yielded no canonical v1 btih (pure-v2 or malformed).
type IngestBatch struct {
	New       int `json:"new"`
	Updated   int `json:"updated"`
	Duplicate int `json:"duplicate"`
	Conflict  int `json:"conflict"`
	Skipped   int `json:"skipped"`
}

// QueueCounts is the durable queue snapshot returned alongside an ingest batch.
type QueueCounts struct {
	Available int `json:"available"`
	Leased    int `json:"leased"`
	Exhausted int `json:"exhausted"`
}

// IngestSummary is the ingest response: the per-call buckets plus the current
// durable queue counts.
//
// Consumers wake their matcher off queue.available rather than the batch
// buckets, so the wake signal survives a lost response.
type IngestSummary struct {
	Batch IngestBatch `json:"batch"`
	Queue QueueCounts `json:"queue"`
}
