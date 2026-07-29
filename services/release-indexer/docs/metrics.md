# Metrics

The release-indexer exposes Prometheus metrics at `/metrics`.

## kura_indexer

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `kura_indexer_build_info` | gauge | `version`, `commit` | Build metadata; value is always `1`. |
| `kura_indexer_http_requests_total` | counter | `method`, `path`, `status` | HTTP requests by routed path. Unknown paths use `path="other"`. |
| `kura_indexer_http_request_duration_seconds` | histogram | `method`, `path` | HTTP request duration. |
| `kura_indexer_ingest_batches_total` | counter | `result` | Ingest batches by `ok` or `error`. |
| `kura_indexer_ingest_posts_total` | counter | `source`, `result` | Ingested posts by source and outcome. Results include `new`, `updated`, `duplicate`, `conflict`, `skipped`, and `error`. |
| `kura_indexer_ingest_batch_size` | histogram | none | Posts per ingest batch. |
| `kura_indexer_source_crawls_total` | counter | `source`, `result` | Scheduled crawls by source and `ok`, `crawl_error`, or `ingest_error`. |
| `kura_indexer_source_crawl_duration_seconds` | histogram | `source` | End-to-end scheduled crawl and ingest duration. |
| `kura_indexer_source_crawl_posts_total` | counter | `source` | Posts returned by scheduled crawls. |
| `kura_indexer_source_last_success_timestamp_seconds` | gauge | `source` | Unix time of the last fully successful scheduled crawl+ingest; `0` until the first success after boot. Panel: `time() - gauge`. |
| `kura_indexer_queue_items` | gauge | `state` | Current release queue/status counts. States are `claimable`, `leased`, `unmatched`, `matched`, `suppressed`, and `exhausted`. |
| `kura_indexer_queue_stats_scrape_ok` | gauge | none | `1` when queue stats were readable during scrape, otherwise `0`. |
| `kura_indexer_catalog_raw_posts` | gauge | none | Current row count in `raw_items`. |
| `kura_indexer_catalog_infohashes` | gauge | none | Current row count in `releases`; one row per unique infohash. |
| `kura_indexer_catalog_refs` | gauge | none | Current number of unique non-empty refs. |
| `kura_indexer_catalog_stats_scrape_ok` | gauge | none | `1` when catalog stats were readable during scrape, otherwise `0`. |
| `kura_indexer_queue_claims_total` | counter | `result` | Queue claim requests by `claimed`, `empty`, or `error`. |
| `kura_indexer_queue_claimed_items_total` | counter | none | Claimed queue items. |
| `kura_indexer_queue_claim_batch_size` | histogram | none | Items per non-empty claim response. |
| `kura_indexer_submit_total` | counter | `status`, `result` | Submit attempts by matcher status and result. Status is `matched`, `unmatched`, `suppressed`, or `invalid`; result is `ok`, `conflict`, or `error`. |
| `kura_indexer_submit_confidence` | histogram | `status` | Confidence values for successful `matched` and `suppressed` submissions. |
| `kura_indexer_submit_unmatched_reasons_total` | counter | `reason` | Successful `unmatched` submissions by normalized matcher reason: `no_candidate`, `ambiguous`, `parse_failure`, `low_confidence`, `none` (no reason sent), or `other` (unrecognized free text). The wire `reason` field stays free text; normalization protects label cardinality. |

Go runtime and process metrics are exported by the single service.

An example Grafana dashboard JSON is available at
[`docs/grafana/release-indexer-dashboard.json`](grafana/release-indexer-dashboard.json). It uses a
Prometheus datasource variable named `datasource`.
