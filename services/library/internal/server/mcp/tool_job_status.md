Look up the current status of an async job submitted by `kura_scan`, `kura_stage`, or `kura_reconcile_apply`. Returns the job's state, latest progress event, and on terminal state either the workflow result or the failure reason.

`state` values:
- `running`: job is in flight; `progress` is the latest emitted event (`phase`, `status`, `message`, `current`, `total`). Absent only briefly at job start before the first event fires.
- `succeeded`: job finished; `result` carries the workflow's typed return.
- `failed`: job finished with an error; `error` carries kind + message + structured data.

`progress.current` / `progress.total` advance with each step for all job kinds (`scan`, `stage`, `reconcile_apply`). Poll until `state` is terminal; don't assume the job is stuck if `progress` hasn't changed — some steps (large file moves, NFS) take longer than the poll interval.

Polling cadence is up to the caller. Terminal jobs stay in memory for `KURA_JOB_RETENTION` (default `30m`) and may also be readable from persisted job logs until log retention removes them. A lookup returns `not_found` only when neither source has the job ID.

<!-- schema-note
Parameter schema is defined in tool_job_status.go (jsonschema tags on jobStatusInput struct).
That Go definition is authoritative. If this section conflicts with the Go file, the Go file wins.
-->
<!-- schema -->
## Parameters


- `jobId` (string, required) — job ID returned by `kura_scan`, `kura_stage`, or `kura_reconcile_apply` (26-char Crockford base32 ULID).
<!-- /schema -->
