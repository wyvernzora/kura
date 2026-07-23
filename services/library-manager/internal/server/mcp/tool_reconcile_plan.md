Compute the file moves `kura_reconcile_apply` would perform on this series and persist them as a plan. Returns the plan's `token` plus the changes it would make. Apply revalidates the planned snapshot; if series state changes, re-plan before applying.

A change describes one media-file move: kind (`add` / `replace` / `move`), episode slot, current path (`from`), target canonical path (`to`), source / resolution, and any companion-file moves. `move` is a canonical rename or move of an existing active record. `replaced` is set when the move displaces an existing active record.

`trashItems` lists files that will disappear from their original location and move into recoverable Kura trash on apply; the trash destination is intentionally hidden from MCP. `extras` lists extras that will be placed under `Season N/Extra/[prefix]/`.

Preview paths are descriptive, not generic selectors to pass to other tools. In-series paths are relative to the series root. Inbox paths are shown as `inbox:<rel>` selectors. Other external absolute paths are reduced to basenames so MCP output does not leak host paths.

Returns `changes: []` and no token when there is nothing to do.

<!-- schema-note
Parameter schema is defined in tool_reconcile_plan.go (jsonschema tags on reconcilePlanInput struct).
That Go definition is authoritative. If this section conflicts with the Go file, the Go file wins.
-->
<!-- schema -->
## Parameters


- `ref` (string, required) — metadata ref (e.g. `tvdb:370070`) from `kura_resolve`.
<!-- /schema -->

The plan covers all three kinds of staged items: episode moves, trash removals, and extras placements.
