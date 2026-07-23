# kura-webui

The kura suite's web UI: a static SPA served by Caddy, which also
reverse-proxies `/api/*` to the service APIs — one origin for the
browser, no CORS, one auth point.

- Build: `make build` (Vite). Dev: `make dev`.
- Image: `kura-webui` — SPA bundle + Caddyfile, no custom backend.
- Upstreams via env: `KURA_WEBUI_LIBRARY_UPSTREAM` (e.g. `kura-server:8080`).
- `src/api/types.gen.ts` is generated from the library service's response
  shapes (`make -C ../library gen-ts`); CI enforces drift.

Today the SPA covers the library service; releases and backup views land
here as those services grow their surfaces.
