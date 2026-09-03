# syntax=docker/dockerfile:1

ARG GO_VERSION=1.22
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build

WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY *.go ./

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
RUN set -eux; \
    export CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH"; \
    if [ "$TARGETARCH" = "arm" ]; then export GOARM="${TARGETVARIANT#v}"; fi; \
    go build -trimpath -buildvcs=false -ldflags="-s -w" -o /out/simple-cloudflare-ddns .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates \
    && addgroup -S ddns \
    && adduser -S -G ddns ddns
COPY --from=build /out/simple-cloudflare-ddns /usr/local/bin/simple-cloudflare-ddns

USER ddns
ENTRYPOINT ["/usr/local/bin/simple-cloudflare-ddns"]
