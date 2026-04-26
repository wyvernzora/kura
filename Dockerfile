FROM golang:1.24.5-alpine AS build

WORKDIR /src

COPY go.mod ./
COPY cmd ./cmd

RUN go build -trimpath -ldflags="-s -w" -o /out/kura ./cmd/kura

FROM alpine:3.22

RUN addgroup -S kura && adduser -S -G kura kura

USER kura

COPY --from=build /out/kura /usr/local/bin/kura

ENTRYPOINT ["kura"]
