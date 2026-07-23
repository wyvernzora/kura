## Build stage
ARG BUILDPLATFORM=linux/amd64
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build

WORKDIR /src

# Pre-fetch dependencies for layer caching.
COPY go.mod go.sum ./
RUN go mod download

# Copy the whole release-indexer module.
COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /out/app \
    ./cmd/kura-release-indexer

## Runtime stage
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/app /usr/local/bin/app
COPY config.example.toml /etc/kura/release-indexer.toml

EXPOSE 8080
USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/app"]
