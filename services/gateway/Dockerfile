# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM node:24-alpine AS web-build
WORKDIR /src
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM caddy:2-alpine
ARG VERSION=dev
# The suite version the Caddyfile stamps onto every response. Baked from the
# same ARG that stamps the bridge, so the two cannot disagree.
ENV KURA_VERSION=${VERSION}
RUN setcap -r /usr/bin/caddy \
    && chown -R 65532:65532 /config /data
COPY Caddyfile /etc/caddy/Caddyfile
COPY --chown=65532:65532 --from=web-build /src/dist /srv
USER 65532:65532
EXPOSE 8080
