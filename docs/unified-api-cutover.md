# Unified API cutover runbook

The code side of the unified API migration is complete and on
`feature/unified-api`. What remains is deployment: Pomerium, the PostgreSQL
schema and data copy, and the K2 manifests. All of it changes running
infrastructure, so it is deliberately operator-driven rather than automated.

This runbook is the ordered procedure. The ordering is not stylistic — each
constraint below exists because the obvious order fails in a specific way.

## Before you start

**Step 0 is a hard precondition, not a warm-up.** Configure and verify the
Pomerium browser-session route for the public hostname against a throwaway
upstream *first*.

The services no longer contain any authentication. It was not disabled behind a
flag; it was deleted, and the gateway additionally strips inbound
`Authorization` before proxying. There is therefore nothing to turn back on if
Pomerium is misconfigured — the only remedy is redeploying an older image.
Deploying the gateway before Pomerium is proven leaves `DELETE /api/library/v1/trash`
and the release ingest and queue routes anonymous to anyone who can reach the
hostname.

Non-browser access is out of scope. The CLI reaches the services through
`kubectl port-forward` until its own OAuth handshake lands — note that this
bypasses both Pomerium and the network policy, so it is an operator escape
hatch rather than a supported client path.

## Procedure

1. **Quiesce legacy Takuhai.** Keep the legacy workload at zero replicas with
   its public ingress removed. Note that "quiesced" for the *new*
   release-indexer means sources disabled and no public traffic — not zero
   replicas, because step 3 needs it running to create tables.

2. **Create the schema.** `CREATE SCHEMA releases` in database `kura`, granted
   to the `release_indexer` role.

3. **Network policy first, then deploy.** Apply the release-indexer network
   policy *before* creating its Service. The service has no authentication of
   its own, so a Service that exists ahead of its policy leaves
   `/api/releases/v1/ingest` and `/api/releases/v1/queue/*` writable by any pod
   in the cluster for the entire migration window.

   Then deploy the library-manager, release-indexer, and gateway Deployments
   and the leaf Services under their new names, with every release-indexer
   source disabled, and scale release-indexer up.

   Startup runs the Goose migrations into the empty schema. There is no
   migrate-only mode — the binary takes only `-config` and `-version` — so this
   step *is* the migration run.

   Leave `service/kura` and the ingress alone. The Service rename is the
   hostname flip and belongs to step 8; doing it here publishes an empty
   release index during the copy.

4. **Copy the data** with explicit per-table `\copy`, in foreign-key order:
   `releases`, then `raw_items`, then `match_events`. Do not copy
   `goose_db_version` — the schema is already at head.

   `pg_dump`/`pg_restore` cannot do this. `--data-only` emits `COPY` statements
   qualified with the *source* schema, and `search_path` does not rewrite a
   schema-qualified name, so the restore either fails outright or silently
   lands rows in `public` while the service reads `releases`. Dump each table
   to CSV and `\copy releases.<table>` back.

5. **Reset the sequences.** `setval` on `releases.raw_items_id_seq` and
   `releases.match_events_id_seq` to the copied maxima. `\copy` does not
   advance them, so the first insert after cutover would collide.

6. **Verify** row counts and a few representative release records against
   schema `releases`.

7. **Wait for readiness** of the library-manager Pods from step 3, under a
   transitional network policy that still permits the current ingress path.
   This is a readiness barrier, not a config rollout — those Pods already carry
   the auth-free image and have no gate to switch.

8. **Flip the hostname and tighten the policy in one apply.** Rename the
   library Service off `kura`, point `service/kura` at the gateway, switch the
   ingress, and move to the final network policy together.

   Order matters because the Service rename takes effect immediately while a
   leaf rollout does not: flipping before the new Pods are ready serves the
   public hostname from a mix of old and new. Tightening the policy before the
   flip would cut the still-live direct ingress path — which is why step 7's
   rule is transitional.

   Leaf MCP dies here, atomically with the ingress switch. The manifests delete
   the per-service MCP Services, remove the MCP ingress target, and forbid
   proxy-to-leaf traffic, so leaf MCP is externally unreachable the moment they
   land regardless of what the phase ordering says elsewhere.

9. **Enable release-indexer sources.**

10. **Deploy the migrated n8n nodes.** Replace the separate library and release
    credentials with one `Kura API` credential pointed at the gateway, then
    recreate or update workflows against the type-version 2 nodes.

11. **Fix forward in place** for anything discovered.

## Why the gateway can go out early

The gateway tolerates release-indexer being absent. Before it is scaled up:

- the web UI and library REST stay usable;
- the library MCP tools stay usable;
- release routes and release tools return `backend_unavailable`;
- the tool catalog stays complete — a backend outage never changes
  `tools/list`, because an agent that cannot see a tool concludes the
  capability does not exist;
- release operations start succeeding when release-indexer becomes healthy,
  with no gateway restart.

## Configuration the deployment must supply

Declared once per leaf, on the gateway Pod. Caddy interpolates them and the
bridge reads the same two variables, so routing and the bridge cannot desync:

```text
KURA_LIBRARY_UPSTREAM=kura-library-manager:8080
KURA_RELEASES_UPSTREAM=kura-release-indexer:8080
```

Leaf Services publish `8080` directly, matching the container port — the
network policy matches the post-DNAT pod port, and two numbers in two places is
how that drifts.

Also required:

- `server.metrics_addr` on release-indexer, distinct from `server.addr`, with
  Prometheus pointed at it. Startup rejects an empty value or one equal to
  `addr`.
- No `[auth]` table in `library-manager.toml`. Strict decoding fails startup on
  a leftover.
- Gateway liveness and readiness on `/healthz`, which has no backend
  dependency. Do not point them at `/api/v1/health` — that reports leaf state,
  and a database outage would restart the gateway.

`gateway.toml` is currently baked into the image at `/etc/kura/gateway.toml`. If
you want it ConfigMap-mounted, mount over that path; the bridge takes
`-config`.

## Validation left for the live stack

These require live-hostname or shutdown behavior beyond the local product-stack
suite:

- job SSE first-event latency, and that progress events are not buffered until
  connection close;
- MCP Streamable HTTP over both POST and GET/SSE through the real hostname;
- bounded shutdown with MCP and SSE sessions open.

## After cutover

The service images already contain no leaf MCP servers. Confirm that the old
per-service MCP Services and ingress targets are absent, then remove any
temporary migration policy and retire the legacy Takuhai deployment after the
copied data and enabled crawlers have been observed in production.
