<div align="center">
    <br>
    <img width="256" src="services/library/docs/assets/logo-full-256.png">
    <h1 align="center">蔵</h1>
</div>

<p align="center">
<b>kura — anime-first library automation suite</b>
</p>

<hr>

One repo, several services, one version line:

| Component | What it does |
|---|---|
| [`services/library`](services/library/) | The library manager: scans, stages, and reconciles an anime library; REST + MCP + embedded web UI |
| [`services/releases`](services/releases/) | Release indexer: durable store + match work queue over raw releases (DMHY, Nyaa crawlers) |
| [`integrations/n8n`](integrations/n8n/) | Custom n8n nodes for both services — n8n drives the acquisition pipeline |
| `services/backup` *(planned)* | LTO tape archival service |
| `services/webui` *(planned)* | Suite-wide web UI |

Everything ships as `kura-*` container images versioned together (`vX.Y.Z`
tags). Development: `make check` fans out to every service; per-service
Makefiles are the fast path. Commit subjects follow Conventional Commits
with a closed scope enum — see [AGENTS.md](AGENTS.md).

MIT © Denis Luchkin-Zhou
