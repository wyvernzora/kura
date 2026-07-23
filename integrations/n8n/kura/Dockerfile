# Kura n8n nodes init-container image.
#
# Stage 1 compiles the TypeScript package. Stage 2 copies only package.json
# and dist/ into a tiny image whose entrypoint installs the package into the
# N8N_CUSTOM_EXTENSIONS volume.
#
# Built from the repo root so docs/assets/logo-n8n.svg is in scope for the
# build-time icon copy.

ARG VERSION=dev

FROM node:24-alpine AS build
WORKDIR /src/integrations/n8n/kura
ENV CI=true
RUN corepack enable
COPY integrations/n8n/kura/package.json integrations/n8n/kura/pnpm-lock.yaml integrations/n8n/kura/pnpm-workspace.yaml ./
RUN corepack pnpm install --frozen-lockfile
COPY integrations/n8n/kura/ ./
COPY services/library-manager/docs/assets/logo-n8n.svg /src/services/library-manager/docs/assets/logo-n8n.svg
RUN corepack pnpm build
RUN mkdir -p /pkg && cp -r package.json dist /pkg/

FROM busybox:1.38-musl
LABEL org.opencontainers.image.source="https://github.com/wyvernzora/kura"
COPY --from=build /pkg /kura-nodes
COPY integrations/n8n/kura/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENV KURA_NODES_TARGET=/opt/n8n/custom
ENTRYPOINT ["/entrypoint.sh"]
