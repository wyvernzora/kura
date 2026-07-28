FROM golang:1.26-alpine AS build

WORKDIR /src
COPY cmd/fake-upstreams/main.go .
RUN CGO_ENABLED=0 go build -trimpath -o /out/fake-upstreams main.go

FROM alpine:3.24
COPY --from=build /out/fake-upstreams /usr/local/bin/fake-upstreams
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/fake-upstreams"]
