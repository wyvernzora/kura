# Combined dev container

One container, three transports, hot reload. Runs `kura serve --rest +
--mcp-http` plus the bundled `@modelcontextprotocol/inspector` UI.
Replaces the prior `tools/inspector/` and `tools/restdev/` images.

## Quick start

```sh
make devserver-build       # one-time: build the image
make devserver-run         # foreground: container with hot reload

# First start auto-generates a bearer token at /var/lib/kura/token
# and prints it in the boot log. Copy it into KURA_TOKEN on the host:
export KURA_SERVER_URL=http://127.0.0.1:8080
export KURA_TOKEN=<copied-from-container-stderr>
kura list
kura add tvdb:370070       # if KURA_TVDB_KEY set on the container
```

For zero-friction dev (no bearer gate), pass `KURA_DISABLE_TOKEN=1`:

```sh
KURA_DISABLE_TOKEN=1 KURA_DEV_STUBS=1 make devserver-run
```

The container also prints a copy-paste inspector URL on first start
once kura's MCP HTTP transport is bound. The URL embeds the proxy
session token, the kura bearer token, and the prefill query params
(`transport=streamable-http`, `serverUrl=http://127.0.0.1:8081/mcp`).
Open it in a browser, click **Connect**.

## What runs inside

| Process | Role |
|---|---|
| `air` (foreground) | Watches `/src/internal` and `/src/cmd` for `.go` changes; rebuilds + restarts kura on save (~3s end-to-end). |
| `kura serve` (managed by air) | Serves REST on `0.0.0.0:8080` and MCP HTTP on `127.0.0.1:8081` simultaneously. |
| `mcp-inspector` (background) | Browser UI on `0.0.0.0:6274`, proxy on `0.0.0.0:6277`. Connects to kura's MCP HTTP endpoint via loopback. |
| `mediainfo` (binary on PATH) | Runtime dependency of `kura scan`. |

`tini -g` reaps all three when docker stops the container.

## Modes

### Stub provider (no TVDB key needed)

```sh
KURA_DEV_STUBS=1 make devserver-run
```

Compiles with `-tags=e2e_stub` and passes `--use-test-stubs`. The
container reads its provider/inspector from `internal/teststub/`. Use
`kura add stub:1001` to add the canned fixture series.

### Real TVDB

```sh
KURA_TVDB_KEY=... make devserver-run
```

Resolver / spine fetch hits the real TVDB API.

### Persistent library

Default is an ephemeral container directory under `/mnt/library`. For
a persistent library that survives container restart, set
`KURA_LIBRARY_ROOT` (same env var the host CLI uses):

```sh
export KURA_LIBRARY_ROOT=$HOME/Media/anime
make devserver-run
```

The directory is bind-mounted at `/mnt/library` inside the container;
permissions follow the container's default UID (root, so the bind
mount is writable regardless of host UID). Setting `KURA_LIBRARY_ROOT`
to a non-existent path errors out before docker runs.

### Different REST port

```sh
REST_DEV_PORT=9090 make devserver-run
# Then: export KURA_SERVER_URL=http://127.0.0.1:9090
```

`INSPECTOR_PORT` and `INSPECTOR_PROXY_PORT` similarly override the
inspector UI / proxy host ports (default `6274` / `6277`).

### Forwarded env vars

`make devserver-run` forwards the following kura env vars from the
host shell into the container when set:

| Env var | Use |
|---|---|
| `KURA_LIBRARY_ROOT` | Bind-mount source for `/mnt/library`. |
| `KURA_INBOX_ROOT` | Host path bind-mounted at `/mnt/inbox` (download staging area, e.g. qBittorrent output). Container always sets `KURA_INBOX_ROOT=/mnt/inbox` via Dockerfile ENV; if you don't bind a host path, the in-container `/mnt/inbox` starts empty. |
| `KURA_TVDB_KEY` | Real TVDB credentials. Skip when `KURA_DEV_STUBS=1`. |
| `KURA_DEV_STUBS` | `1` enables stub provider + inspector (e2e_stub build). |
| `KURA_PREFERRED_LANGUAGES` | BCP-47 list, e.g. `en,ja`. |
| `KURA_LOG_LEVEL` | `DEBUG` is gold for tracing handler flow. |
| `KURA_REST_CORS_ORIGINS` | Comma-separated origin allow-list (browser dev). |
| `KURA_TOKEN` | Skip auto-generation; use this literal as the bearer token. |
| `KURA_DISABLE_TOKEN` | `1` disables the bearer gate entirely (proxy-fronted dev / friction-free poking). |

## Ports

| Port | Role | Default host bind |
|---|---|---|
| 8080 | REST API | `127.0.0.1:$REST_DEV_PORT` |
| 8081 | Kura MCP HTTP transport | `127.0.0.1:$MCP_HTTP_PORT` |
| 6274 | Inspector UI (browser) | `127.0.0.1:$INSPECTOR_PORT` |
| 6277 | Inspector proxy (browser ↔ MCP) | `127.0.0.1:$INSPECTOR_PROXY_PORT` |

## Use with local MCP clients

The project ships a `.mcp.json` entry for `kura` that points at
`http://127.0.0.1:8081/mcp` and reads the bearer token from `KURA_TOKEN`:

```json
"kura": {
  "type": "http",
  "url": "http://127.0.0.1:8081/mcp",
  "headers": {
    "Authorization": "Bearer ${KURA_TOKEN}"
  }
}
```

To use it:

```sh
make devserver-build
make devserver-run                  # logs the bearer token on first start

# in a second shell:
export KURA_TOKEN=<copied-from-container-stderr>
```

Point your MCP client at the project `.mcp.json` entry, then confirm
that `kura` is connected and listing tools (`kura_list`, `kura_show`,
`kura_scan`, ...).

For zero-friction usage (skip the bearer gate):

```sh
KURA_DISABLE_TOKEN=1 make devserver-run
# .mcp.json works as-is; auth middleware is a passthrough when the
# token is unset, so the Authorization header value is ignored.
```

Override the host port if 8081 is taken:

```sh
MCP_HTTP_PORT=9081 make devserver-run
# then update .mcp.json url to http://127.0.0.1:9081
```

## How reload works

1. air watches `/src/internal/` and `/src/cmd/` via polling
   (~500ms). macOS bind mounts don't deliver inotify events
   reliably to Linux containers; polling is the portable choice.
2. On `.go` save, air runs `go build -tags=$BUILD_TAGS -o /src/tmp/kura ./cmd/kura-library-manager`.
3. air sends SIGTERM to the running kura process; restarts it
   via `run-kura.sh` which reassembles `--rest` + `--mcp-http` args.
4. End-to-end ~3s for a small change.
5. Inspector keeps running across kura restarts; click **Reconnect**
   in the UI if the proxy lost the previous connection.

## Auth

Two layers, both enabled by default:

1. **Inspector proxy ↔ browser.** Per-session token guard built into
   Inspector. The startup banner prints the URL with that token
   embedded; do not disable (`DANGEROUSLY_OMIT_AUTH=true`) — the proxy
   is reachable from the browser, and unguarded means any visited site
   can drive the proxy on your behalf.

2. **Kura ↔ Inspector proxy / Kura ↔ host CLI.** Kura's bearer-token
   deploy gate. The container honors the same resolution order as
   `kura serve`: `KURA_DISABLE_TOKEN=1` > `KURA_TOKEN=<value>` >
   generated and persisted at `/var/lib/kura/token`. The entrypoint
   reads the resolved token and appends `&bearerToken=<token>` to the
   prefilled inspector URL, so clicking Connect just works without
   manual paste.

## Files

- `Dockerfile` — golang:alpine + air + nodejs + mcp-inspector + mediainfo + tini.
- `.air.toml` — watch + build config.
- `run-kura.sh` — air's bin entry; assembles `--rest` + `--mcp-http` from env.
- `entrypoint.sh` — sets `BUILD_TAGS`, prints inspector URL once kura binds, starts mcp-inspector + air.
- `README.md` — this file.

## Limitations

- Container REST bind is `0.0.0.0:8080`; the host port mapping
  (default `127.0.0.1:$REST_DEV_PORT`) is the security boundary.
  Don't expose `0.0.0.0:8080` on the host without a proxy in front.
- Initial container start spends ~10-20s on first `go build` (cold
  module cache). Subsequent rebuilds are fast.
- `_test.go` files are excluded from the watch; editing tests doesn't
  trigger a server rebuild.
- Inspector reconnect after a hot-reload may need a manual click; the
  proxy doesn't auto-resume on its own.
