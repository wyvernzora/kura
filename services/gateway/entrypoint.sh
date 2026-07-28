#!/bin/sh
# Gateway container entrypoint: Caddy and the MCP bridge share one container.
#
# tini -g is PID 1 with both processes in its group. The wrapper tracks both
# PIDs and waits on them: `kill 0` would signal this shell too, and tini exits
# as soon as its immediate child does — so the container would tear down before
# either process finished draining.
set -eu

caddy run --config /etc/caddy/Caddyfile &
c=$!
kura-mcp-bridge --config /etc/kura/gateway.toml &
b=$!

stop() { kill -TERM "$c" "$b" 2>/dev/null || true; }
trap stop TERM INT

# Returns when either child exits, or when a trapped signal arrives. Either
# child exiting takes the Pod down and Kubernetes restarts it; there is
# deliberately no restart policy or intentional-versus-unexpected distinction
# here.
wait -n || true
stop
wait "$c" "$b" 2>/dev/null || true
