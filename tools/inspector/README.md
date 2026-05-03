# MCP Inspector container

Visual end-to-end harness for the kura MCP tool surface. Bundles
`kura serve --mcp-http` and `@modelcontextprotocol/inspector` in one
image so the agent's view of kura can be exercised from a browser
without installing node, npm, or mediainfo on the host.

## Build

```sh
make inspector-build
# equivalent: docker build -f tools/inspector/Dockerfile -t kura-inspector .
```

## Run

```sh
make inspector-run LIBRARY_ROOT=/path/to/library
# equivalent:
# docker run --rm -it \
#   -p 6274:6274 -p 6277:6277 \
#   -v /path/to/library:/mnt/library \
#   -e KURA_TVDB_KEY="..." \
#   kura-inspector
```

`LIBRARY_ROOT` defaults to `./testlib` if not set; create the directory
or override.

The container prints an inspector URL with an embedded session token on
startup. Open it in a browser; the connection form will be prefilled
with `http://127.0.0.1:8080` (the in-container kura HTTP transport).
Click **Connect** to attach.

## Pass-through env vars

Anything the kura CLI honors via env propagates with `-e KEY=VALUE` on
`docker run`. Common ones:

| Var | Purpose |
|---|---|
| `KURA_TVDB_KEY` | TVDB v4 API key (required for resolve / add / scan). |
| `KURA_MEDIAINFO_COMMAND` | Override the bundled `mediainfo` binary. |
| `KURA_INDEX_PROBE_INTERVAL` | Cache probe cadence (default `2s`). |
| `KURA_JOB_TIMEOUT` | Per-job deadline for async tools. |
| `KURA_CONFLICT_RETRIES` | CAS retry count for short writes. |

`KURA_LIBRARY_ROOT` is fixed to `/mnt/library` inside the container;
override only if the mount target changes.

## Ports

| Port | Role |
|---|---|
| 6274 | Inspector UI (browser) |
| 6277 | Inspector proxy (browser ↔ MCP) |
| 8080 | Kura MCP HTTP transport (in-container; not published) |

## File ownership

The container runs as `root` so the bind-mounted `/mnt/library` is
writable regardless of host UID. Files kura creates (`.kura/...`)
will be owned by root on the host. Pass `--user "$(id -u):$(id -g)"`
to `docker run` to write as your host user instead — but make sure
the library directory permissions allow that UID first.

## Auth

Inspector ships with a per-session token guard. The startup banner
prints the URL with the token embedded; do not skip auth in this
container — the proxy is reachable from the browser, and disabling
auth (`DANGEROUSLY_OMIT_AUTH=true`) lets any visited site drive the
proxy via your browser.
