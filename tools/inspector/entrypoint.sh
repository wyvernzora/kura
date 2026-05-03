#!/bin/sh
# Launch kura HTTP transport in the background, then run mcp-inspector
# in the foreground. tini (PID 1) reaps both when the container stops.
#
# We pin a session token up front (instead of letting inspector mint a
# random one) so the entrypoint can print a copy-paste URL with the
# token + UI prefill query params (transport + serverUrl).

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
CLIENT_PORT="${CLIENT_PORT:-6274}"

# Pin token so we can print the prefill URL. /dev/urandom + tr is more
# portable than openssl rand.
if [ -z "${MCP_PROXY_AUTH_TOKEN}" ]; then
  MCP_PROXY_AUTH_TOKEN="$(tr -dc 'a-f0-9' < /dev/urandom | head -c 64)"
  export MCP_PROXY_AUTH_TOKEN
fi

kura serve --mcp-http="${KURA_HTTP_ADDR}" &
KURA_PID=$!

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

cat <<EOF >&2
entrypoint: kura serving MCP at http://${KURA_HTTP_ADDR}
entrypoint: open the inspector UI at:
  http://localhost:${CLIENT_PORT}/?MCP_PROXY_AUTH_TOKEN=${MCP_PROXY_AUTH_TOKEN}&transport=streamable-http&serverUrl=http%3A%2F%2F${KURA_HTTP_ADDR%:*}%3A${KURA_HTTP_ADDR#*:}
EOF

# Inspector reads MCP_PROXY_AUTH_TOKEN, HOST, CLIENT_PORT, SERVER_PORT,
# ALLOWED_ORIGINS, MCP_AUTO_OPEN_ENABLED from env. UI prefill comes
# from query params on the URL above; CLI args here would only matter
# in --cli mode.
exec mcp-inspector
