#!/bin/sh
# Launch kura HTTP transport in the background, then run mcp-inspector
# in the foreground. tini (PID 1) reaps both when the container stops.

set -e

if [ -z "${KURA_LIBRARY_ROOT}" ]; then
  echo "entrypoint: KURA_LIBRARY_ROOT not set" >&2
  exit 1
fi
if [ ! -d "${KURA_LIBRARY_ROOT}" ]; then
  echo "entrypoint: ${KURA_LIBRARY_ROOT} is not a directory (mount the library at /mnt/library)" >&2
  exit 1
fi

KURA_HTTP_ADDR="${KURA_HTTP_ADDR:-127.0.0.1:8080}"

kura serve --mcp-http="${KURA_HTTP_ADDR}" &
KURA_PID=$!

# Trap so SIGINT/SIGTERM from tini propagates to kura before we exit.
trap 'kill -TERM "${KURA_PID}" 2>/dev/null; wait "${KURA_PID}" 2>/dev/null' INT TERM

# Wait briefly for kura to bind. mcp-inspector will fail-fast on the
# first connect attempt if kura isn't listening yet.
i=0
while ! nc -z "${KURA_HTTP_ADDR%:*}" "${KURA_HTTP_ADDR#*:}" 2>/dev/null; do
  i=$((i + 1))
  if [ "${i}" -gt 30 ]; then
    echo "entrypoint: kura did not bind ${KURA_HTTP_ADDR} within 3s" >&2
    kill "${KURA_PID}" 2>/dev/null || true
    exit 1
  fi
  sleep 0.1
done

echo "entrypoint: kura serving MCP over http://${KURA_HTTP_ADDR}"
echo "entrypoint: starting inspector UI on http://0.0.0.0:${CLIENT_PORT:-6274}"

# Inspector picks up server URL from --server-url; the UI prefills the
# connection form with this. User still clicks "Connect" once the page
# loads.
exec mcp-inspector --transport http --server-url "http://${KURA_HTTP_ADDR}"
