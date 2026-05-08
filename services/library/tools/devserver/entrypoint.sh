#!/bin/sh
# Bootstraps mcp-inspector and air against the bind-mounted source
# tree. air manages the kura subprocess (rebuild + restart on .go
# change); mcp-inspector runs alongside and proxies the browser UI to
# kura's MCP HTTP transport.
#
# tini -g (PID 1, set in ENTRYPOINT) forwards SIGTERM to the whole
# process group so all three (entrypoint shell, air+kura, inspector)
# die together when docker stops the container.

set -e

if [ ! -d "/src/cmd/kura" ]; then
  echo "entrypoint: /src/cmd/kura missing — bind-mount the kura repo at /src" >&2
  exit 1
fi
if [ ! -d "${KURA_LIBRARY_ROOT}" ]; then
  echo "entrypoint: KURA_LIBRARY_ROOT=${KURA_LIBRARY_ROOT} not a directory" >&2
  exit 1
fi

# Build tag selection. Stub mode pulls in the in-memory provider +
# inspector via teststub package; production mode uses the real
# tvdb client and host mediainfo.
if [ "${KURA_DEV_STUBS:-}" = "1" ]; then
  export BUILD_TAGS="e2e_stub"
  echo "devserver: stub mode — provider/inspector swapped via teststub"
else
  export BUILD_TAGS=""
fi

# Pin inspector proxy auth token up front so we can print a copy-paste
# URL with the token + UI prefill query params (transport, serverUrl,
# bearerToken).
if [ -z "${MCP_PROXY_AUTH_TOKEN}" ]; then
  MCP_PROXY_AUTH_TOKEN="$(tr -dc 'a-f0-9' < /dev/urandom | head -c 64)"
  export MCP_PROXY_AUTH_TOKEN
fi

REST_PORT="${KURA_REST_ADDR##*:}"
MCP_HOST="${KURA_MCP_HTTP_ADDR%:*}"
MCP_PORT="${KURA_MCP_HTTP_ADDR##*:}"

cat <<EOF >&2
devserver: REST     listening on container 0.0.0.0:${REST_PORT}
devserver: MCP HTTP listening on container ${MCP_HOST}:${MCP_PORT}
devserver: from host  →  export KURA_SERVER_URL=http://127.0.0.1:\$REST_DEV_PORT
devserver: edit any .go file under cmd/ or internal/ and air rebuilds in ~3s
EOF

if [ "${KURA_DISABLE_TOKEN:-}" = "1" ] || [ "${KURA_DISABLE_TOKEN:-}" = "true" ]; then
  echo "devserver: bearer-token gate disabled (KURA_DISABLE_TOKEN)"
elif [ -n "${KURA_TOKEN:-}" ]; then
  echo "devserver: using bearer token from KURA_TOKEN env var"
else
  echo "devserver: bearer token will be generated at /var/lib/kura/token on first start"
fi

# Prefill-URL printer. Backgrounded so air can exec in the foreground.
# Waits for kura to bind the MCP HTTP port, resolves the bearer token
# (which may have just been generated), and prints the inspector URL
# with all prefill query params attached. Runs once per container
# start; air's restart of kura on .go save does not retrigger this
# (the URL stays valid as long as ports + token don't change).
(
  # Probe via loopback regardless of bind addr — works for both
  # 127.0.0.1:PORT and 0.0.0.0:PORT.
  i=0
  while ! nc -z 127.0.0.1 "${MCP_PORT}" 2>/dev/null; do
    i=$((i + 1))
    if [ "${i}" -gt 600 ]; then
      echo "devserver: kura did not bind ${KURA_MCP_HTTP_ADDR} within 60s; skipping inspector URL" >&2
      exit 0
    fi
    sleep 0.1
  done

  # Resolve the kura bearer token to embed in the inspector prefill
  # URL. Mirrors auth.Load's resolution order: KURA_DISABLE_TOKEN
  # bypass > KURA_TOKEN literal > /var/lib/kura/token.
  KURA_BEARER=""
  case "${KURA_DISABLE_TOKEN:-}" in
    1|true|TRUE|yes|on)
      : ;;
    *)
      if [ -n "${KURA_TOKEN:-}" ]; then
        KURA_BEARER="${KURA_TOKEN}"
      elif [ -f /var/lib/kura/token ]; then
        KURA_BEARER="$(tr -d '\r\n' < /var/lib/kura/token)"
      fi
      ;;
  esac

  # serverUrl is the inspector proxy's view of kura — always reach
  # via loopback inside the container, regardless of kura's bind addr.
  INSPECTOR_URL="http://localhost:${CLIENT_PORT}/?MCP_PROXY_AUTH_TOKEN=${MCP_PROXY_AUTH_TOKEN}&transport=streamable-http&serverUrl=http%3A%2F%2F127.0.0.1%3A${MCP_PORT}"
  if [ -n "${KURA_BEARER}" ]; then
    INSPECTOR_URL="${INSPECTOR_URL}&bearerToken=${KURA_BEARER}"
  fi

  cat <<EOF >&2
devserver: open the inspector UI at:
  ${INSPECTOR_URL}
EOF
) &

mkdir -p /src/tmp

# mcp-inspector reads MCP_PROXY_AUTH_TOKEN, HOST, CLIENT_PORT,
# SERVER_PORT, ALLOWED_ORIGINS, MCP_AUTO_OPEN_ENABLED from env.
# Backgrounded so air can run in the foreground; tini -g cleans
# both up on container stop.
mcp-inspector &

# Web tooling: Vite (full app) + Storybook (component sandbox). Both
# run against the bind-mounted /src/web. node_modules lives in an
# anonymous docker volume so host (macOS) binaries don't clobber the
# alpine ones via the bind mount; on first start we trigger a fresh
# pnpm install into the volume.
case "${KURA_WEB_DISABLED:-}" in
  1|true|TRUE|yes|on)
    echo "devserver: KURA_WEB_DISABLED set — skipping Vite + Storybook"
    ;;
  *)
    if [ ! -d /src/web ]; then
      echo "devserver: /src/web not found — skipping Vite + Storybook"
    else
      if [ ! -d /src/web/node_modules/.pnpm ]; then
        echo "devserver: /src/web/node_modules empty — running pnpm install"
        (cd /src/web && pnpm install --frozen-lockfile) || \
          echo "devserver: pnpm install failed; web tooling will not start" >&2
      fi
      if [ -d /src/web/node_modules/.pnpm ]; then
        # macOS bind-mounts (VirtioFS / gRPC-FUSE) don't propagate
        # inotify events into the Linux container, so Vite + Storybook
        # never see source-file edits and HMR is silently broken.
        # CHOKIDAR_USEPOLLING flips chokidar (the watcher both tools
        # share) to a polling loop. 300 ms keeps CPU burn modest while
        # feeling instant. WATCHPACK_POLLING covers Storybook's webpack
        # builder for older addon versions.
        export CHOKIDAR_USEPOLLING=1
        export CHOKIDAR_INTERVAL=300
        export WATCHPACK_POLLING=true
        (cd /src/web && pnpm dev) &
        (cd /src/web && pnpm storybook) &
        echo "devserver: Vite on container :5173, Storybook on container :6006 (polling watcher)"
      fi
    fi
    ;;
esac

# air config lives at /etc/kura/air.toml so the /src bind mount
# doesn't shadow it.
exec air -c /etc/kura/air.toml
