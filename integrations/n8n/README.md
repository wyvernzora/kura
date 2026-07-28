# n8n-nodes-kura

Custom n8n nodes for the Kura suite — one package, one action node, one
trigger:

- **Kura** node — resources × operations over the suite APIs:
  - **Series** (library-manager): List actionable series, Show series
    state (with an optional second "untracked" output that resolves
    unknown refs), Update Tags.
  - **Release** (release-indexer): Ingest raw posts, Get a release, Get
    a magnet link.
  - **Queue** (release-indexer): Claim a batch, Submit dispositions
    (single body, array, `items`, or structured-output `output.items`;
    409s surface as `ok: false` conflicts), Get queue stats.
    All resources use the **Kura API** credential and reach the leaf services
    through the unified gateway.
- **Kura Queue Trigger** — polling trigger that claims release-indexer
  queue work on n8n's standard poll schedule and stays idle (no
  execution) when nothing is claimable. Kept separate from the action
  node because n8n triggers are structurally distinct node types.

The package targets the v2 node contract: one gateway base URL, canonical
`ref` fields, camelCase request and response fields, and `/api/v1` routes.
Version 1 workflows must be migrated before installing this package.

## Workflow migration

- Replace `Kura Library API` and `Kura Releases API` credentials with one
  `Kura API` credential whose base URL points at the gateway.
- Replace node parameters and expressions named `metadataRef` with `ref`.
- Replace `lease_seconds` with `leaseSeconds`.
- Re-add or update each Kura node to type version 2. The package intentionally
  does not retain the pre-gateway v1 transport.

## Build

```sh
pnpm install
pnpm build       # tsc + icon copy (needs services/library-manager/docs/assets in scope)
pnpm test        # projection and request-contract checks against compiled output
```

## Image

Built as a minimal init container (`ghcr.io/wyvernzora/kura/n8n-nodes`)
from the repo root context. On start, the entrypoint copies the built
package into the volume n8n scans via `N8N_CUSTOM_EXTENSIONS`
(`KURA_NODES_TARGET`, default `/opt/n8n/custom`). Versioned in lockstep
with the service images by the repo-wide release workflow.
