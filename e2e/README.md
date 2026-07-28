# Product end-to-end tests

This module owns tests whose assertions cross Kura component boundaries. The
suite builds and runs the production Dockerfiles for the gateway,
library-manager, and release-indexer together with PostgreSQL 18. Controlled
fake servers replace only external TVDB and release-source traffic.

Run it from the repository root:

```sh
make product-e2e
```

Service suites remain next to their owners. They are the exhaustive contracts
for service behavior and deploy shape; this module does not repeat every route
through the gateway.

## Deferred web UI coverage

TODO: add a Playwright suite against the production gateway image. It should
exercise rendered user journeys rather than duplicate REST assertions, and
retain screenshots, traces, console errors, and failed network requests as CI
artifacts. Browser automation is intentionally outside the current test sweep.
