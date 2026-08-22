# Dev container: hot-reload `kura serve` against a bind-mounted source
# tree. REST is the service's only transport — the suite MCP surface
# lives in the gateway, and the SPA has its own tooling under
# services/gateway/web.
#
# Build: `make devserver-build`
# Run:   `make devserver-run`
#
# Wire KURA_DEV_STUBS=1 to compile with -tags=e2e_stub and pass
# --use-test-stubs (in-memory provider); leave unset for the real
# provider path (KURA_TVDB_KEY required for verbs that resolve against
# TVDB).
FROM golang:1.26.6-alpine

# mediainfo is a runtime dep of `kura scan`. tini reaps air/kura
# cleanly when docker stops the container; -g (set on ENTRYPOINT)
# forwards signals to the whole process group so backgrounded children
# die too. git is needed for `go install` against modules.
RUN apk add --no-cache mediainfo tini git netcat-openbsd \
 && go install github.com/air-verse/air@latest \
 && mkdir -p /mnt/library /mnt/inbox /src

WORKDIR /src

# /src is bind-mounted at runtime, overlaying anything we COPY there.
# Stash air's config + scripts outside /src so they survive the mount.
COPY tools/devserver/.air.toml /etc/kura/air.toml
COPY tools/devserver/library-manager.toml /etc/kura/library-manager.toml
COPY tools/devserver/run-kura.sh /usr/local/bin/run-kura.sh
COPY tools/devserver/entrypoint.sh /entrypoint.sh
RUN chmod +x /usr/local/bin/run-kura.sh /entrypoint.sh

# REST listens on 0.0.0.0 inside the container; the Makefile target
# pins the host-side port to 127.0.0.1 so the bind is not publicly
# reachable — with no auth in kura, that pinning is the only boundary.
EXPOSE 8080

ENTRYPOINT ["/sbin/tini", "-g", "--", "/entrypoint.sh"]
