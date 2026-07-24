#!/bin/sh
# air invokes this as the binary path; the actual kura binary built
# by air's build step lives at /src/tmp/kura.
#
# Set KURA_DEV_STUBS=1 (via docker run -e) to enable --use-test-stubs.

set -e

EXTRA_ARGS=""
if [ "${KURA_DEV_STUBS:-}" = "1" ]; then
  EXTRA_ARGS="--use-test-stubs"
fi

# Bind safety lives in the bearer-token gate. 0.0.0.0 inside the
# container is fine because requests without a valid token return
# 401, and the host port mapping pins to 127.0.0.1 by default
# (Makefile target).
exec /src/tmp/kura \
  --config=/etc/kura/library-manager.toml \
  ${EXTRA_ARGS}
