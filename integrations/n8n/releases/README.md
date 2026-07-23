# Release-indexer n8n nodes

Custom n8n nodes for the Kura release indexer. Crawling and crawl scheduling run
inside `kura-release-indexer`; n8n only drives the external matcher loop and may
push posts through `POST /ingest` when an out-of-process producer is useful.

## Nodes

| Node | Kind | Credential | What it does |
| --- | --- | --- | --- |
| **Takuhai** | action | Takuhai API | Ingest posts, inspect releases and magnets, claim queue work, submit dispositions, and read queue stats. |
| **Takuhai Queue Trigger** | trigger | Takuhai API | Poll `/queue/claim` and emit one batch of claimed releases. |

Operation cardinalities:

- **Releases → Ingest** forwards one `posts` batch to `/ingest` and emits one
  summary item.
- **Releases → Get Release** emits the release detail unchanged.
- **Releases → Get Magnet Link** emits `{infohash, magnet}`.
- **Queue → Claim** emits one `{items, count}` item.
- **Queue → Submit Dispositions** accepts one disposition, an array, one
  `{items}` batch, or structured-output `{output:{items}}`; it emits compact
  per-item results.
- **Queue → Get Queue Stats** emits one stats item.
- **Takuhai Queue Trigger** emits one `{items, count}` item per non-empty poll.

Claim and ingest preserve the server envelopes so new response fields flow
through without a node change.

## Packaging

The package is built as the `ghcr.io/wyvernzora/kura-releases-n8n-nodes` init
image. At pod startup it copies the compiled nodes into the `emptyDir` mounted
at `N8N_CUSTOM_EXTENSIONS`.

## Development

```sh
corepack enable
corepack pnpm install --frozen-lockfile
corepack pnpm build
```

The build copies the shared release-indexer icon beside every compiled node and
credential.
