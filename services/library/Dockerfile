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

# VERSION is stamped into the kura binary via -ldflags="-X main.Version=...".
# Pass at build time: `docker build --build-arg VERSION=v0.1.0 ...`. Falls
# back to "dev" so ad-hoc `docker build` without the arg still produces a
# usable image; published images should always set this.
ARG VERSION=dev

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

# Bring the freshly built web bundle into the embed package's dist/
# subtree. The Go embed directive picks it up at compile time.
COPY --from=web /src/web/dist/. ./internal/server/webui/dist/

# CGO_ENABLED=0 forces a fully static binary so the final distroless
# layer doesn't need musl/glibc shims for kura itself; only mediainfo
# pulls in shared libs in the final image.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.Version=${VERSION}" -o /out/kura ./cmd/kura

# Mediainfo runtime stage. Debian provides a glibc-linked mediainfo
# binary plus its shared-library closure. We materialize a minimal
# rootfs at /opt/rootfs containing only mediainfo, its libs, and the
# pre-chowned /var/lib/kura tree, then COPY the whole rootfs into the
# distroless final stage in one shot.
FROM debian:stable-slim AS mediainfo

ARG KURA_UID=10001
ARG KURA_GID=10001

# Avoid apt prompts; install only what mediainfo needs to run.
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        mediainfo \
 && rm -rf /var/lib/apt/lists/*

# Stage mediainfo + every shared library it dynamically links to into
# /opt/rootfs at their original absolute paths. The dynamic linker
# resolves DT_NEEDED entries from the same paths in the final image,
# so no LD_LIBRARY_PATH or rpath rewriting is required.
#
# ldd's output formats vary: "name => /abs/path (0x...)" for resolved
# libs, "/abs/path (0x...)" for the linker stub. The awk extracts the
# absolute path in either case; the explicit dynamic-linker glob
# catches /lib*/ld-linux-*.so.* which sometimes shows up as the
# bracketed "name (0x...)" form without a "=>" arrow.
RUN mkdir -p /opt/rootfs \
 && (ldd /usr/bin/mediainfo | awk '/=> \//{print $3}; /^\t\//{print $1}'; \
     ls /lib/*-linux-*/ld-linux-*.so.* 2>/dev/null) \
        | sort -u > /tmp/lib-list \
 && tar -C / -hcf - --files-from=/tmp/lib-list /usr/bin/mediainfo \
        | tar -C /opt/rootfs -xf -

# Pre-create /var/lib/kura inside the staging rootfs with the requested
# ownership. distroless has no shell so we can't chown at runtime;
# this empty tree ships into the final image and inherits the recorded
# UID/GID.
RUN mkdir -p /opt/rootfs/var/lib/kura \
 && chown -R ${KURA_UID}:${KURA_GID} /opt/rootfs/var/lib/kura

FROM gcr.io/distroless/cc-debian12:latest

# UID / GID baked into the image. Override per build to match the
# UID/GID your NFS export expects:
#   docker build \
#     --build-arg KURA_UID=$(id -u) \
#     --build-arg KURA_GID=$(id -g) \
#     -t kura .
# Keep the same values across builder and final stages so /var/lib/kura
# ownership inside the image matches the runtime USER.
ARG KURA_UID=10001
ARG KURA_GID=10001

# Mediainfo + shared libs + pre-chowned /var/lib/kura, all at their
# canonical paths. Single COPY keeps layers minimal.
COPY --from=mediainfo /opt/rootfs/ /

# kura binary. Static (CGO_ENABLED=0) so it has no shared-library deps
# beyond what cc-debian12 already ships (glibc, libssl, ca-certs).
COPY --from=go-build /out/kura /usr/local/bin/kura

# Volume for the persisted bearer token. Mount a PVC / Docker volume
# to retain across pod restarts, or inject KURA_TOKEN from a Secret to
# bypass the file path entirely.
VOLUME ["/var/lib/kura"]

# Default REST bind port. Adjust if `--rest=:NNNN` differs.
EXPOSE 8080

# Mediainfo's shared libs ship in arch-specific /lib/$arch/ paths.
# distroless's bundled /etc/ld.so.cache only indexes the libs that
# came with cc-debian12; libmediainfo et al. were added later and
# aren't cached. There's no shell or ldconfig to rebuild the cache,
# so we point LD_LIBRARY_PATH at both arch dirs we ever care about
# (only the matching arch resolves at runtime; the other path is
# silently ignored). Multi-arch builds keep working with one ENV.
ENV LD_LIBRARY_PATH=/lib/aarch64-linux-gnu:/usr/lib/aarch64-linux-gnu:/lib/x86_64-linux-gnu:/usr/lib/x86_64-linux-gnu

# Run as the build-time UID:GID. distroless has no /etc/passwd entry
# for arbitrary UIDs; the numeric form skips name lookup.
USER ${KURA_UID}:${KURA_GID}

# No HEALTHCHECK directive: distroless ships no shell or wget. Use
# Kubernetes httpGet probes against /api/v1/health for liveness /
# readiness. Same path, no extra binary needed.

ENTRYPOINT ["/usr/local/bin/kura"]
