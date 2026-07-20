# Kura n8n nodes

Custom n8n nodes for Kura. The package is built as an init-container image
that installs into the directory n8n reads from `N8N_CUSTOM_EXTENSIONS`.

## Node

| Node | What it does |
| --- | --- |
| Kura | Resource **Series**, operations **List**, **Show**, and **Update Tags**. |

**Series → List** intentionally skips `error` and `untracked` rows. They are
not download-actionable.

**Series → Show** reads one series by `metadataRef`. With **Error on Not Found**
enabled, the node has one output. With it disabled, the node has **tracked** and
**untracked** outputs; missing tracked series are resolved by metadata ref and
emitted as one candidate on **untracked**. Tracked Show output always includes
`tags` as an array, using `[]` when the series has no tags.

**Series → List** accepts a space-delimited tag filter. Plain tags must be
present and `!tag` expressions must be absent; all expressions compose with
AND semantics.

**Series → Update Tags** accepts space-delimited tag changes. Plain tags add
to the series and `!tag` expressions remove from it. Kura normalizes tags to
lowercase and treats them as opaque workflow markers. Current Takuhai
conventions are:

- `maintenance:requested`
- `maintenance:disabled`
- `priority:high`
- `priority:low`

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
