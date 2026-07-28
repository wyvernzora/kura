#!/bin/sh
# Bootstraps air against the bind-mounted source tree; air manages the
# kura subprocess, rebuilding and restarting it on any .go change.
#
# tini -g (PID 1, set in ENTRYPOINT) forwards SIGTERM to the whole
# process group so both the entrypoint shell and air+kura die together
# when docker stops the container.

set -e

if [ ! -d "/src/cmd/kura-library-manager" ]; then
  echo "entrypoint: /src/cmd/kura-library-manager missing — bind-mount the kura repo at /src" >&2
  exit 1
fi
if [ ! -d /mnt/library ]; then
  echo "entrypoint: /mnt/library not a directory" >&2
  exit 1
fi
if [ ! -d /mnt/inbox ]; then
  echo "entrypoint: /mnt/inbox not a directory" >&2
  exit 1
fi

# Build tag selection. Stub mode pulls in the in-memory provider via the
# teststub package; production mode uses the real tvdb client and host
# mediainfo.
if [ "${KURA_DEV_STUBS:-}" = "1" ]; then
  export BUILD_TAGS="e2e_stub"
  echo "devserver: stub mode — provider swapped via teststub"
else
  export BUILD_TAGS=""
fi

REST_PORT=8080

cat <<EOF >&2
devserver: REST listening on container 0.0.0.0:${REST_PORT}
devserver: from host  →  export KURA_SERVER_URL=http://127.0.0.1:\$REST_DEV_PORT
devserver: edit any .go file under cmd/ or internal/ and air rebuilds in ~3s
EOF

mkdir -p /src/tmp

# air config lives at /etc/kura/air.toml so the /src bind mount
# doesn't shadow it.
exec air -c /etc/kura/air.toml
