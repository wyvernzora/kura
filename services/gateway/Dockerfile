# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM node:24-alpine AS web-build
WORKDIR /src
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM --platform=$BUILDPLATFORM golang:1.26.4-alpine AS go-build
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/kura-mcp-bridge ./cmd/kura-mcp-bridge

FROM caddy:2-alpine
ARG VERSION=dev
# The suite version the Caddyfile stamps onto every response, from the same ARG
# that stamps the bridge's ldflags — so the header and /api/v1/health agree.
# Do not override this at runtime: the bridge bakes the same value into its
# ldflags, and a runtime-only override would make the header disagree with
# /api/v1/health.
ENV KURA_VERSION=${VERSION}
# caddy:2-alpine ships neither tini nor bash; /bin/sh is busybox ash.
RUN apk add --no-cache tini \
    && setcap -r /usr/bin/caddy \
    && mkdir -p /etc/kura \
    && chown -R 65532:65532 /config /data
COPY Caddyfile /etc/caddy/Caddyfile
COPY gateway.toml /etc/kura/gateway.toml
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
COPY --from=go-build /out/kura-mcp-bridge /usr/local/bin/kura-mcp-bridge
COPY --chown=65532:65532 --from=web-build /src/dist /srv
USER 65532:65532
EXPOSE 8080 9090 9091
ENTRYPOINT ["/sbin/tini", "-g", "--", "/usr/local/bin/entrypoint.sh"]
