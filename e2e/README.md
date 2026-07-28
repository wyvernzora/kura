# Product end-to-end tests

This module owns tests whose assertions cross Kura component boundaries. The
suite builds and runs the production Dockerfiles for the gateway,
library-manager, release-indexer, and n8n nodes together with PostgreSQL 18
and the pinned production n8n runtime. Controlled fake servers replace only
external TVDB and release-source traffic.

Run it from the repository root:

```sh
make product-e2e
```

Service suites remain next to their owners. They are the exhaustive contracts
for service behavior and deploy shape; this module does not repeat every route
through the gateway.

The n8n coverage imports real credentials and workflows into n8n. One workflow
executes the action node across release queue and library operations. A second
publishes the polling trigger and lets a live n8n server claim and submit work.
Both workflows talk only to the product gateway.

## Deferred web UI coverage

TODO: add a Playwright suite against the production gateway image. It should
exercise rendered user journeys rather than duplicate REST assertions, and
retain screenshots, traces, console errors, and failed network requests as CI
artifacts. Browser automation is intentionally outside the current test sweep.
