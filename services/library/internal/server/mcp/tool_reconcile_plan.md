Compute the file moves `kura_reconcile_apply` would perform on this series and persist them as a plan. Returns the plan's `token` plus the changes it would make. Plans expire 5 minutes after creation; re-call to get a fresh one.

A change describes one media-file move: kind (`add` / `replace` / `refresh`), episode slot, current path (`from`), target canonical path (`to`), source / resolution, and any companion-file moves. `replaced` is set when the move displaces an existing active record.

`trashItems` lists files that will be removed by apply (only `from` is exposed; the file is gone after apply runs). `extras` lists extras that will be placed under `Season N/Extra/[prefix]/`.

Returns `changes: []` and no token when there is nothing to do.

<!-- schema-note
Parameter schema is defined in tool_reconcile_plan.go (jsonschema tags on reconcilePlanInput struct).
That Go definition is authoritative. If this section conflicts with the Go file, the Go file wins.
-->
## Parameters

<!-- schema -->
- `ref` (string, required) — metadata ref (e.g. `tvdb:370070`) from `kura_resolve`.
<!-- /schema -->

The plan covers all three kinds of staged items: episode moves, trash removals, and extras placements.
