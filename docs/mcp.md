# MCP tool surface

Kura exposes its workflow surface as MCP tools for AI agents. Two
transports:

- `kura serve --mcp-stdio` — stdio MCP, for agents launched as a child
  process. Unauthenticated; the process boundary already trusts the parent.
- `kura serve --mcp-http=:port` — MCP-over-HTTP. Auth gated by the
  same bearer token as REST (see [rest-api.md](rest-api.md#auth)).

For underlying terms, see [concepts.md](concepts.md). For each tool's
journey, see [lifecycle.md](lifecycle.md).

## Tools

12 tools registered in `internal/server/mcp/server.go`. Each tool has
a tool-specific markdown brief alongside its handler
(`internal/server/mcp/tool_<name>.md`); those briefs are what the
agent sees.

| Tool | Input | Output | Sync / async |
|------|-------|--------|--------------|
| `kura_resolve` | `{terms: string[]}` | Candidate list | sync |
| `kura_list` | `{statuses?, airing?, maxResults?, cursor?}` | Paginated series list | sync |
| `kura_show` | `{ref, episodes?, status[], source[], resolution[]?}` | Series + episodes detail (no trash) | sync |
| `kura_add` | `{ref, dirname?, ordering?}` | Add result | sync |
| `kura_import` | `{ref, dirname, ordering?}` | Import result | sync |
| `kura_stage` | `{ref, episodes[], trash[], extras[]}` | Stage result | sync |
| `kura_reset` | `{ref, episode?, trash[], extras[], all?}` | `{cleared, trashRemoved[], extraRemoved[]}` | sync |
| `kura_reconcile_plan` | `{ref}` | `{token, changes[], trashItems[], extras[]}` | sync |
| `kura_scan` | `{ref, refresh?, ordering?}` | `{jobId}` | async |
| `kura_reconcile_apply` | `{ref, token}` | Job handle | async |
| `kura_job_status` | `{jobId}` | Status + progress + result/error | sync |
| `kura_inbox_list` | `{maxResults?, cursor?}` | Paginated inbox files | sync |

Async tools return a job handle; the agent polls `kura_job_status`.
See [lifecycle.md §Async jobs](lifecycle.md#async-jobs).

## Agent safety properties

The MCP surface is designed so an agent operating against Kura
**cannot cause permanent data loss, regardless of agent behavior**.
Specifically:

- **Read tools are non-destructive.** `kura_resolve`, `kura_list`,
  `kura_show`, `kura_inbox_list`, `kura_job_status` cannot mutate
  state.
- **Intent expression is metadata-only.** `kura_stage` and
  `kura_reset` write or remove staged records; no files move.
- **Reconcile displaces, never deletes.** `kura_reconcile_apply`
  moves displaced active files to `<series>/.kura/trash/<ulid>/`
  with self-describing `meta.json`; restoration is filesystem-only
  via `trash restore` (CLI / REST operator).
- **Scan re-derives metadata from filesystem reality.** Does not
  modify files.
- **Trash is invisible on agent surfaces.** `kura_show` omits trash
  data; no `trash list / empty / restore` tools exist.
- **Permanent deletion is operator-only.** `trash empty` and
  `remove --purge` require explicit CLI invocation, or REST with
  operator + confirm headers. Operator review is the durability
  boundary.

The same property protects against bugs in Kura itself. Reconcile is
the most complex mutating operation; bugs in plan generation or apply
execution have happened and will happen again. Trash makes those
bugs recoverable.

## Disambiguation

Selectors in MCP tools follow the same resolution protocol as CLI
and REST. When `kura_resolve`, `kura_show`, etc. encounter multiple
candidates, the response includes a structured candidate set with
provider-supplied metadata (title, year, status, genres) and per-term
match evidence. The agent picks one and re-invokes with the
MetadataRef as the sole term. See
[concepts.md §Disambiguation](concepts.md#disambiguation-as-a-cross-cutting-pattern).
