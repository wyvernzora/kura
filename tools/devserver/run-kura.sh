#!/bin/sh
# air invokes this as the binary path; the actual kura binary built
# by air's build step lives at /src/tmp/kura. We assemble the serve
# args here so air's bin/args_bin doesn't have to encode env-driven
# conditionals.
#
# Set KURA_DEV_STUBS=1 (via docker run -e) to enable --use-test-stubs.
# KURA_REST_ADDR defaults to 0.0.0.0:8080 (set in Dockerfile ENV).
# KURA_MCP_HTTP_ADDR defaults to 127.0.0.1:8081 (loopback inside
# container — inspector proxy reaches it locally).

set -e

EXTRA_ARGS=""
if [ "${KURA_DEV_STUBS:-}" = "1" ]; then
  EXTRA_ARGS="--use-test-stubs"
fi

# Bind safety lives in the bearer-token gate. 0.0.0.0 inside the
# container is fine because requests without a valid token return
# 401, and the host port mapping pins to 127.0.0.1 by default
# (Makefile target).
exec /src/tmp/kura serve \
  --rest="${KURA_REST_ADDR}" \
  --mcp-http="${KURA_MCP_HTTP_ADDR}" \
  ${EXTRA_ARGS}
