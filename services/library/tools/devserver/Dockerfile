# Combined dev container: hot-reload kura serve with REST + MCP HTTP
# transports plus the bundled @modelcontextprotocol/inspector UI.
# Replaces tools/inspector/ and tools/restdev/.
#
# Build: `make devserver-build`
# Run:   `make devserver-run`
#
# Wire KURA_DEV_STUBS=1 to compile with -tags=e2e_stub and pass
# --use-test-stubs (in-memory provider + inspector); leave unset for
# the real provider path (KURA_TVDB_KEY required for verbs that
# resolve against TVDB).
FROM golang:1.26.2-alpine

# nodejs/npm host the mcp-inspector. mediainfo is a runtime dep of
# `kura scan`. tini reaps air/inspector/kura subprocesses cleanly when
# docker stops the container; -g (set on ENTRYPOINT) forwards signals
# to the whole process group so backgrounded children die too. git
# is needed for `go install` against modules.
RUN apk add --no-cache mediainfo tini git nodejs npm netcat-openbsd \
 && npm install -g @modelcontextprotocol/inspector@latest \
 && go install github.com/air-verse/air@latest \
 && mkdir -p /mnt/library /mnt/inbox /src /var/lib/kura

WORKDIR /src

# /src is bind-mounted at runtime, overlaying anything we COPY there.
# Stash air's config + scripts outside /src so they survive the mount.
COPY tools/devserver/.air.toml /etc/kura/air.toml
COPY tools/devserver/run-kura.sh /usr/local/bin/run-kura.sh
COPY tools/devserver/entrypoint.sh /entrypoint.sh
RUN chmod +x /usr/local/bin/run-kura.sh /entrypoint.sh

# REST and MCP HTTP both listen on 0.0.0.0 inside the container; the
# Makefile target pins the host-side ports to 127.0.0.1 so binds are
# not publicly reachable. The bearer-token gate (auth middleware) is
# the actual security boundary. 6274 = inspector UI, 6277 = inspector
# proxy (browser ↔ MCP).
ENV KURA_LIBRARY_ROOT=/mnt/library \
    KURA_INBOX_ROOT=/mnt/inbox \
    KURA_REST_ADDR=0.0.0.0:8080 \
    KURA_MCP_HTTP_ADDR=0.0.0.0:8081 \
    HOST=0.0.0.0 \
    CLIENT_PORT=6274 \
    SERVER_PORT=6277 \
    MCP_AUTO_OPEN_ENABLED=false \
    ALLOWED_ORIGINS=http://localhost:6274,http://127.0.0.1:6274

EXPOSE 8080 8081 6274 6277

ENTRYPOINT ["/sbin/tini", "-g", "--", "/entrypoint.sh"]
