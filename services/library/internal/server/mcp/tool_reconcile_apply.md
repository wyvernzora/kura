Execute a reconcile plan: move staged files into their canonical locations and archive any displaced active records to trash. Returns a `jobId`.

Apply can take seconds to minutes for large plans (file moves on NFS). It runs as a tracked job.

Plans expire 5 minutes after creation by `kura_reconcile_plan`. An expired token returns `plan_expired`; re-call `kura_reconcile_plan` for a fresh one. A token whose plan was already applied returns `plan_applied` (treat as success-equivalent).

If the series state changed between plan and apply, apply returns `stale_snapshot`. Re-call `kura_reconcile_plan` and try again.

<!-- schema-note
Parameter schema is defined in tool_reconcile_apply.go (jsonschema tags on reconcileApplyInput struct).
That Go definition is authoritative. If this section conflicts with the Go file, the Go file wins.
-->
## Parameters

<!-- schema -->
- `ref` (string, required) — metadata ref (e.g. `tvdb:370070`) from `kura_resolve`.
- `token` (string, required) — plan token from `kura_reconcile_plan` (12-char hex).
<!-- /schema -->

## Failure and concurrency notes

While a reconcile job is running, other mutating tools on the same series may be blocked — wait for terminal state before retrying.

If apply fails mid-flight and a follow-up call complains the series is busy, the user needs to clear the stale state via the CLI (`kura reconcile recover <ref>`) — not exposed through MCP. Surface it to the user.
