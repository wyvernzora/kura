Execute a reconcile plan: move staged files into their canonical locations and archive any displaced active records to trash. Returns a `jobId`.

Apply can take seconds to minutes for large plans (file moves on NFS). It runs as a tracked job.

Plan-read failures surface through `kura_job_status` as terminal `error.kind` values. `plan_applied` means the token was already applied (treat as success-equivalent). `not_found` means the plan token is missing; call `kura_reconcile_plan` again.

If the series state changed between plan and apply, job status reports `stale_snapshot`. Re-call `kura_reconcile_plan` and try again.

<!-- schema-note
Parameter schema is defined in tool_reconcile_apply.go (jsonschema tags on reconcileApplyInput struct).
That Go definition is authoritative. If this section conflicts with the Go file, the Go file wins.
-->
<!-- schema -->
## Parameters


- `ref` (string, required) — metadata ref (e.g. `tvdb:370070`) from `kura_resolve`.
- `token` (string, required) — plan token from `kura_reconcile_plan` (12-char hex).
<!-- /schema -->

## Failure and concurrency notes

While a reconcile job is running, other mutating tools on the same series may be blocked — wait for terminal state before retrying.

If apply fails mid-flight and a follow-up call complains the series is busy, the user needs to clear the stale state via the CLI (`kura reconcile recover <ref>`) — not exposed through MCP. Surface it to the user.
