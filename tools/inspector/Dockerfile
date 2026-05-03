# Visual MCP testing harness. Bundles `kura serve --mcp-http` plus
# `@modelcontextprotocol/inspector` so the kura tool surface can be
# exercised end-to-end from a browser without setting anything up on
# the host. See tools/inspector/README.md for usage.

FROM golang:1.26.2-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/kura ./cmd/kura

FROM node:22-alpine

# mediainfo is a runtime dep of `kura scan`. tini reaps the kura +
# inspector subprocesses cleanly when docker stops the container.
RUN apk add --no-cache mediainfo tini netcat-openbsd \
 && npm install -g @modelcontextprotocol/inspector@latest \
 && mkdir -p /mnt/library

COPY --from=build /out/kura /usr/local/bin/kura
COPY tools/inspector/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# Runs as root by default so the bind-mounted /mnt/library is
# writable regardless of host UID. Override with `docker run --user`
# if running against a library you don't want touched as root.

ENV KURA_LIBRARY_ROOT=/mnt/library \
    KURA_HTTP_ADDR=127.0.0.1:8080 \
    HOST=0.0.0.0 \
    CLIENT_PORT=6274 \
    SERVER_PORT=6277 \
    MCP_AUTO_OPEN_ENABLED=false

# 6274 = inspector UI, 6277 = inspector proxy. Both must be published.
EXPOSE 6274 6277

VOLUME ["/mnt/library"]

ENTRYPOINT ["/sbin/tini", "--", "/entrypoint.sh"]
