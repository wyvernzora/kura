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
| `kura_library_index_rebuilding` | gauge | none | `1` while the library index is rebuilding, else `0`. Rebuilds are when `server_not_ready` (HTTP 503) responses occur. |
| `kura_library_index_series` | gauge | none | Series currently tracked in the library index. |

Go runtime and process metrics are exported alongside.
