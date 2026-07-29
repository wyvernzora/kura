# Metrics

The library-manager exposes Prometheus metrics at `/metrics` on a dedicated
listener (`server.metrics`, default `:9090`). The listener serves nothing
else, so granting Prometheus scrape access does not grant API access.

## kura_library

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `kura_library_build_info` | gauge | `version` | Build metadata; value is always `1`. |
| `kura_library_http_requests_total` | counter | `method`, `route`, `status` | HTTP requests by routed pattern. Unmatched requests use `route="other"`. |
| `kura_library_http_request_duration_seconds` | histogram | `method`, `route` | HTTP request duration. |
| `kura_library_jobs_running` | gauge | none | Async jobs currently running. |
| `kura_library_jobs_total` | counter | `kind`, `state` | Terminal async jobs by kind (`scan`, `stage`, `apply`, …) and state (`succeeded`, `failed`). |
| `kura_library_jobs_duration_seconds` | histogram | `kind` | Async job wall-clock duration. |
| `kura_library_index_rebuilding` | gauge | none | `1` while the library index is rebuilding, else `0`. Only *cold* rebuilds (empty index — first boot or corruption recovery) produce `server_not_ready` 503s; the hourly warm rebuild flips this gauge without ever failing a request, so don't alert on the gauge alone. |
| `kura_library_index_rebuild_duration_seconds` | histogram | none | Successful index rebuild duration. On cold starts this is the length of the 503 window; warm rebuilds serve normally throughout. |
| `kura_library_index_series` | gauge | none | Rows in the library index — all statuses, including `untracked` and `error` rows. Equals `sum(kura_library_series_status)`. |
| `kura_library_series_staged` | gauge | none | Series with any staged work awaiting `reconcile apply`: staged episodes (including upgrades over an active file and season-0 specials), staged trash, or staged extras. |
| `kura_library_episodes` | gauge | `state` | Trackable non-special episodes across the library (season-0 specials are excluded from all episode counters, matching the list rollup): `present` (active file), `pending_apply` (staged, no active file), `missing` (aired, no file, nothing staged). The three states partition the total, so a staged *upgrade* over an existing file counts as `present` — watch `kura_library_series_staged` for pending upgrades. |
| `kura_library_series_status` | gauge | `status` | Series by rolled-up list status (`untracked`, `complete`, `incomplete`, `error`). All four statuses are always exported. |
| `kura_library_series_airing` | gauge | none | Series currently observed as airing (independent of status). |
| `kura_library_series_resolution` | gauge | `resolution` | Series with at least one active file at this resolution (`1080p`, `4K`, …). A series counts once per distinct resolution, so the sum can exceed the series total. |
| `kura_library_series_source` | gauge | `source` | Series with at least one active file from this source (`WebRip`, `BDRip`, …). Same multi-count caveat as resolution. |

Go runtime and process metrics are exported alongside.
