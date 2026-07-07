# Kura n8n nodes

Custom n8n nodes for Kura. The package is built as an init-container image
that installs into the directory n8n reads from `N8N_CUSTOM_EXTENSIONS`.

## Node

| Node | What it does |
| --- | --- |
| Kura | Resource **Series**, operations **List** and **Show**. |

**Series → List** intentionally skips `error` and `untracked` rows. They are
not download-actionable.

**Series → Show** reads one series by `metadataRef`. With **Error on Not Found**
enabled, the node has one output. With it disabled, the node has **tracked** and
**untracked** outputs; missing tracked series are resolved by metadata ref and
emitted as one candidate on **untracked**.

## Development

```sh
corepack enable
corepack pnpm install --frozen-lockfile
corepack pnpm typecheck
corepack pnpm build            # tsc -> dist/ + node icon
```

All nodes and credentials share one icon:
[`docs/assets/logo-n8n.svg`](../../docs/assets/logo-n8n.svg). The build copies it into
each compiled node dir and credentials dir. The container image builds from the repo
root so that asset is in scope.
