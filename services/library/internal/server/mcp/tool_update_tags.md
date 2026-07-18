Atomically update opaque workflow tags on one tracked series. Kura normalizes tags to lowercase, validates their syntax, and assigns no meaning to individual values.

Plain expressions add tags. Prefix an expression with `!` to remove that tag. Adding an existing tag and removing an absent tag are idempotent no-ops. Do not pass both `tag` and `!tag` in the same call.

<!-- schema-note
Parameter schema is defined in tool_update_tags.go. That Go definition is authoritative.
-->

## Parameters

- `ref` (string, required) — exact metadata ref, e.g. `tvdb:370070`.
- `tags` ([]string, required) — one or more changes, e.g. `["priority", "!maintenance-disabled"]`.

## Response

Returns `metadataRef` and the complete resulting stored `tags` array.
