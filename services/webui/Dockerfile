# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM node:24-alpine AS build
WORKDIR /src
RUN corepack enable
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY . ./
RUN pnpm build

FROM caddy:2-alpine
# VERSION is a no-op arg so every kura image shares the same build-args.
ARG VERSION=dev
COPY Caddyfile /etc/caddy/Caddyfile
COPY --from=build /src/dist /srv
EXPOSE 8080
