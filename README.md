<div align="center">
    <br>
    <img width="256" src="services/library-manager/docs/assets/logo-full-256.png">
    <h1 align="center">蔵</h1>
</div>

<p align="center">
<b>kura — anime-first library automation suite</b>
</p>

<hr>

One repo, several services, one version line:

| Component | What it does |
|---|---|
| [`services/library-manager`](services/library-manager/) | The library manager: scans, stages, and reconciles an anime library; REST + MCP APIs |
| [`services/release-indexer`](services/release-indexer/) | Release indexer: durable store + match work queue over raw releases (DMHY, Nyaa crawlers) |
| [`integrations/n8n`](integrations/n8n/) | Custom n8n nodes for the suite — n8n drives the acquisition pipeline |
| `services/backup` *(planned)* | LTO tape archival service |
| [`services/webui`](services/webui/) | Suite web UI: static SPA + Caddy proxy, one origin over every service API |

Every service ships as a container image under `ghcr.io/wyvernzora/kura/`,
all versioned together (`vX.Y.Z` tags). Development: `make check` fans out
to every service; per-service
Makefiles are the fast path. Commit subjects follow Conventional Commits
with a closed scope enum — see [AGENTS.md](AGENTS.md).

MIT © Denis Luchkin-Zhou
