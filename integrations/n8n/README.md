# Kura n8n nodes

Custom n8n nodes for Kura. The package is built as an init-container image
that installs into the directory n8n reads from `N8N_CUSTOM_EXTENSIONS`.

## Node

| Node | What it does |
| --- | --- |
| Kura | Resource **Series**, operations **List** and **Show**. |

**Series → List** intentionally skips `error` and `untracked` rows. They are
not download-actionable.

**Series → Show** reads one series by `metadataRef` and emits agent-focused
episode/media state.

## Development

```sh
corepack enable
corepack pnpm install --frozen-lockfile
corepack pnpm typecheck
corepack pnpm build
```
