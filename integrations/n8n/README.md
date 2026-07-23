# n8n-nodes-kura

Custom n8n nodes for the Kura suite — one package, one action node, one
trigger:

- **Kura** node — resources × operations over the suite APIs:
  - **Series** (library-manager): List actionable series, Show series
    state (with an optional second "untracked" output that resolves
    unknown metadata refs), Update Tags. Uses the **Kura Library API** credential
    (base URL + optional bearer token).
  - **Release** (release-indexer): Ingest raw posts, Get a release, Get
    a magnet link.
  - **Queue** (release-indexer): Claim a batch, Submit dispositions
    (single body, array, `items`, or structured-output `output.items`;
    409s surface as `ok: false` conflicts), Get queue stats.
    Release/Queue use the **Kura Release Indexer API** credential (base
    URL only — the indexer has no application-level auth by design).
- **Kura Queue Trigger** — polling trigger that claims release-indexer
  queue work on n8n's standard poll schedule and stays idle (no
  execution) when nothing is claimable. Kept separate from the action
  node because n8n triggers are structurally distinct node types.

## Build

```sh
pnpm install
pnpm build       # tsc + icon copy (needs services/library-manager/docs/assets in scope)
pnpm test        # projection checks against the compiled output
```

## Image

Built as a minimal init container (`ghcr.io/wyvernzora/kura/n8n-nodes`)
from the repo root context. On start, the entrypoint copies the built
package into the volume n8n scans via `N8N_CUSTOM_EXTENSIONS`
(`KURA_NODES_TARGET`, default `/opt/n8n/custom`). Versioned in lockstep
with the service images by the repo-wide release workflow.
