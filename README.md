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
| [`cli`](cli/) | `kura` developer/operator CLI: REST client for the suite services; no image |
| [`services/library-manager`](services/library-manager/) | The library manager: scans, stages, and reconciles an anime library; REST API |
| [`services/release-indexer`](services/release-indexer/) | Release indexer: durable store + match work queue over raw releases (DMHY, Nyaa crawlers) |
| [`integrations/n8n`](integrations/n8n/) | Custom n8n nodes for the suite — n8n drives the acquisition pipeline |
| [`services/tape-backup`](services/tape-backup/) *(planned)* | LTO tape archival: peer service with one binary providing a long-lived control plane plus an ephemeral executor that claims the tape drive |
| [`services/gateway`](services/gateway/) | Suite gateway: Caddy + MCP bridge + SPA, one origin over every service API |

The unified API cutover is operator-driven; its ordered procedure is
[docs/unified-api-cutover.md](docs/unified-api-cutover.md).

Every service ships as a container image under `ghcr.io/wyvernzora/kura/`,
all versioned together (`vX.Y.Z` tags). Development: `make check` fans out
to every service; per-service
Makefiles are the fast path. Commit subjects follow Conventional Commits
with a closed scope enum — see [AGENTS.md](AGENTS.md).

MIT © Denis Luchkin-Zhou
