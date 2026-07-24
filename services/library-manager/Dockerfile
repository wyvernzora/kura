# Pinned to $BUILDPLATFORM (the host the build is running on, e.g. the
# amd64 GitHub runner), not $TARGETPLATFORM. Go cross-compiles natively
# via GOOS/GOARCH, so we run the compiler under the runner's native
# arch and retarget the output — no QEMU emulation of `go build`. On a
# multi-arch buildx run this turns a ~10 min QEMU-emulated arm64
# compile into a ~30 s native cross-build.
FROM --platform=$BUILDPLATFORM golang:1.26.5-alpine AS go-build

WORKDIR /src

# VERSION is stamped into the kura binary via -ldflags="-X main.Version=...".
# Pass at build time: `docker build --build-arg VERSION=v0.5.1 ...`. Falls
# back to "dev" so ad-hoc `docker build` without the arg still produces a
# usable image; published images should always set this.
ARG VERSION=dev

# TARGETOS / TARGETARCH are auto-populated by buildx for each platform
# in the matrix (e.g. linux/amd64, linux/arm64). They drive Go's native
# cross-compiler and need to be declared here for the RUN step below
# to see them.
ARG TARGETOS
ARG TARGETARCH

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

# CGO_ENABLED=0 forces a fully static binary so it runs identically on
# musl (Alpine) and glibc hosts without shared-library shims.
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -X main.Version=${VERSION}" -o /out/kura-library-manager ./cmd/kura-library-manager

# Final runtime stage. Alpine instead of distroless: apk pulls
# mediainfo's full dependency closure (libmediainfo + libzen + libcurl
# + libtinyxml2 + locale data) automatically, and a real shell + busybox
# coreutils survive in the image so operators can `kubectl exec` and
# inspect filesystem state when something breaks.
FROM alpine:3.24

# UID / GID baked into the image. Override per build to match the
# UID/GID your NFS export expects:
#   docker build \
#     --build-arg KURA_UID=$(id -u) \
#     --build-arg KURA_GID=$(id -g) \
#     -t kura-library-manager .
ARG KURA_UID=10001
ARG KURA_GID=10001

# mediainfo: media-probe binary kura shells out to.
# ca-certificates: TLS roots for kura's outbound TVDB calls and any
#   future provider HTTP clients. Static Go binary doesn't bundle
#   them, so without this the binary errors on TLS verification.
# tzdata: pods set TZ=America/Los_Angeles etc.; without zoneinfo the
#   timezone resolution silently falls back to UTC.
RUN apk add --no-cache \
        mediainfo \
        ca-certificates \
        tzdata

# Create the unprivileged runtime user + group at the requested UID/GID
# and pre-create /var/lib/kura with that ownership so the bearer-token
# bootstrap can write the persisted token file.
RUN addgroup -S -g ${KURA_GID} kura \
 && adduser -S -D -H -u ${KURA_UID} -G kura kura \
 && mkdir -p /etc/kura /var/lib/kura \
 && chown -R ${KURA_UID}:${KURA_GID} /var/lib/kura

# kura binary. Static (CGO_ENABLED=0) so musl-vs-glibc doesn't matter.
COPY --from=go-build /out/kura-library-manager /usr/local/bin/kura-library-manager
COPY config.example.toml /etc/kura/library-manager.toml

# Volume for the persisted bearer token. Mount a PVC / Docker volume
# to retain across pod restarts, or inject KURA_TOKEN from a Secret to
# bypass the file path entirely.
VOLUME ["/var/lib/kura"]

# Default bind ports: REST on 8080, MCP-over-HTTP on 8081. Adjust the
# mounted config if you bind different ports.
EXPOSE 8080 8081

USER ${KURA_UID}:${KURA_GID}

# No HEALTHCHECK directive: kubelet's httpGet probe against
# /api/v1/health is the canonical liveness/readiness check across
# Docker and Kubernetes. Avoids embedding a probe binary that would
# duplicate kubelet's behavior.

ENTRYPOINT ["/usr/local/bin/kura-library-manager"]

# The bundled config serves REST and MCP-over-HTTP and expects library
# and inbox mounts at /library and /inbox. Mount a replacement at
# /etc/kura/library-manager.toml for deployment-specific settings.
CMD ["serve", "--config=/etc/kura/library-manager.toml"]
