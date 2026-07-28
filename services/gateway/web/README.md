# kura gateway — web UI

The kura suite's web UI: a static SPA built here and served by the
gateway's Caddy, which also reverse-proxies `/api/*` and `/mcp/v1` — one
origin for the browser, no CORS.

This directory is the SPA only. The Caddyfile, the MCP bridge, and the
image that carries all three live one level up in `services/gateway`.

- Build: `make build` (Vite). Dev: `make dev`.
- Image: `kura-gateway`, built from `services/gateway`.
- Upstreams via env, set on the gateway Pod: `KURA_LIBRARY_UPSTREAM` and
  `KURA_RELEASES_UPSTREAM`.
- `src/api/types.gen.ts` is generated from the library service's `pkg/api`
  response shapes (`make -C ../library-manager gen-ts`); CI enforces drift.

Today the SPA covers the library service; releases and backup views land
here as those services grow their surfaces.
