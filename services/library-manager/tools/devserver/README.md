# Library-manager dev container

One container with the hot-reloaded library-manager REST service. The committed
[library-manager.toml](library-manager.toml) serves REST on `:8080`, uses
`/mnt/library` and `/mnt/inbox`, and enables debug logs. Kura does not
authenticate, so the host port mapping stays loopback-only by default.

## Quick start

```sh
make devserver-build
KURA_DEV_STUBS=1 make devserver-run

# In another shell:
export KURA_SERVER_URL=http://127.0.0.1:8080
kura list
kura add stub:1001
```

## What runs inside

| Process | Role |
|---|---|
| `air` | Watches `/src/internal`, `/src/cmd`, and `/src/pkg`; rebuilds and restarts the service. |
| `kura-library-manager` | Loads `/etc/kura/library-manager.toml` and serves REST. |
| `mediainfo` | Runtime dependency of scan workflows. |

The suite MCP server and SPA live under `services/gateway`; they are not part
of this service-local dev container. `tini -g` reaps the process group when
Docker stops the container.

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
| `KURA_DEV_STUBS` | `1` enables the in-memory e2e provider. |
| `REST_DEV_PORT` | Host REST port; default `8080`. |

Edit [library-manager.toml](library-manager.toml) for non-secret server
settings such as roots, preferred languages, CORS, and logging, then rebuild
the dev image.

## Reload and access

Air polls the bind-mounted Go source, rebuilds `/src/tmp/kura`, and runs it
through `run-kura.sh` with the committed config. Kura itself does not
authenticate, so do not expose the REST port beyond loopback.

## Files

- `Dockerfile` — Go, air, mediainfo, and tini.
- `library-manager.toml` — dev runtime settings.
- `.air.toml` — watch and build configuration.
- `run-kura.sh` — air's binary entrypoint.
- `entrypoint.sh` — validates mounts and starts air.

Initial startup includes a cold Go build. Test-file changes do not trigger a
server rebuild.
