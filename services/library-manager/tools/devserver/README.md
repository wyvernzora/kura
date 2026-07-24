# Combined dev container

One container with hot-reloaded Kura, MCP Inspector, Vite, and Storybook.
The committed [library-manager.toml](library-manager.toml) serves REST on
`:8080`, MCP over HTTP on `:8081`, uses `/mnt/library` and `/mnt/inbox`,
enables debug logs, and disables Kura's bearer gate. Host port mappings remain
loopback-only by default.

## Quick start

```sh
make devserver-build
KURA_DEV_STUBS=1 make devserver-run

# In another shell:
export KURA_SERVER_URL=http://127.0.0.1:8080
kura list
kura add stub:1001
```

The container prints a copy-paste Inspector URL once Kura's MCP HTTP transport
binds. The URL includes Inspector's own proxy-session token and the streamable
HTTP endpoint; open it and click **Connect**.

## What runs inside

| Process | Role |
|---|---|
| `air` | Watches `/src/internal` and `/src/cmd`, rebuilds, and restarts Kura. |
| `kura serve` | Loads `/etc/kura/library-manager.toml`; serves REST and MCP HTTP. |
| `mcp-inspector` | Browser UI on `:6274`, proxy on `:6277`. |
| Vite / Storybook | Web UI development servers unless `KURA_WEB_DISABLED=1`. |
| `mediainfo` | Runtime dependency of scan workflows. |

`tini -g` reaps the process group when Docker stops the container.

## Modes

Stub mode needs no TVDB key:

```sh
KURA_DEV_STUBS=1 make devserver-run
```

Real provider mode receives the API key as a secret:

```sh
KURA_TVDB_KEY=... make devserver-run
```

For persistent data, the Make variables below select host bind-mount sources;
they are not library-manager runtime settings:

```sh
KURA_LIBRARY_ROOT=$HOME/Media/anime \
KURA_INBOX_ROOT=$HOME/Downloads \
make devserver-run
```

Without them, `/mnt/library` and `/mnt/inbox` are ephemeral container
directories. A nonexistent host path fails before Docker starts.

## Inputs

| Make variable / environment variable | Use |
|---|---|
| `KURA_LIBRARY_ROOT` | Host directory bind-mounted at `/mnt/library`. |
| `KURA_INBOX_ROOT` | Host directory bind-mounted at `/mnt/inbox`. |
| `KURA_TVDB_KEY` | TVDB secret forwarded to Kura. |
| `KURA_DEV_STUBS` | `1` enables the e2e stub provider and inspector. |
| `KURA_WEB_DISABLED` | `1` skips Vite and Storybook. |
| `REST_DEV_PORT` | Host REST port; default `8080`. |
| `MCP_HTTP_PORT` | Host MCP HTTP port; default `8081`. |
| `INSPECTOR_PORT` | Host Inspector UI port; default `6274`. |
| `INSPECTOR_PROXY_PORT` | Host Inspector proxy port; default `6277`. |

Edit [library-manager.toml](library-manager.toml) for non-secret server
settings such as preferred languages, CORS, logging, or auth posture, then
rebuild the dev image.

## Ports

| Container port | Role | Default host bind |
|---|---|---|
| 8080 | REST API | `127.0.0.1:$REST_DEV_PORT` |
| 8081 | Kura MCP HTTP | `127.0.0.1:$MCP_HTTP_PORT` |
| 6274 | Inspector UI | `127.0.0.1:$INSPECTOR_PORT` |
| 6277 | Inspector proxy | `127.0.0.1:$INSPECTOR_PROXY_PORT` |

## Reload and auth

Air polls the bind-mounted Go source, rebuilds `/src/tmp/kura`, and runs it
through `run-kura.sh` with the committed config. Inspector stays running across
Kura restarts, although its UI may need a manual reconnect.

Inspector's browser-to-proxy session token remains enabled. Kura's own bearer
gate is disabled only in this loopback-oriented dev config. If you expose the
host ports beyond loopback, first enable `auth.disabled = false` and provide
`KURA_TOKEN`.

## Files

- `Dockerfile` — Go, air, Node, MCP Inspector, mediainfo, and tini.
- `library-manager.toml` — dev runtime settings.
- `.air.toml` — watch and build configuration.
- `run-kura.sh` — air's binary entrypoint.
- `entrypoint.sh` — starts Inspector, web tooling, and air.

Initial startup includes a cold Go build. Test-file changes do not trigger a
server rebuild.
