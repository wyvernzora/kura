FROM node:20-alpine AS web

WORKDIR /src/web

# Enable pnpm via the version pinned in package.json#packageManager.
RUN corepack enable

COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

COPY web/ ./

RUN pnpm build

FROM golang:1.26.2-alpine AS go-build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

# Bring the freshly built web bundle into the embed package's dist/
# subtree. The Go embed directive picks it up at compile time.
COPY --from=web /src/web/dist/. ./internal/server/webui/dist/

RUN go build -trimpath -ldflags="-s -w" -o /out/kura ./cmd/kura

FROM alpine:3.22

RUN apk add --no-cache mediainfo \
    && addgroup -S kura \
    && adduser -S -G kura kura

USER kura

COPY --from=go-build /out/kura /usr/local/bin/kura

ENTRYPOINT ["kura"]
