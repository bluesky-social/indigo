FROM golang:1.25-bookworm AS build-env

ENV DEBIAN_FRONTEND=noninteractive
ENV TZ=Etc/UTC
ENV GODEBUG="netdns=go"
ENV GOOS="linux"
ENV GOARCH="amd64"
ENV CGO_ENABLED="1"

# Install FoundationDB client libraries and git
RUN apt-get update \
    && apt-get install -y --no-install-recommends git \
    && wget -O /tmp/fdb-clients.deb https://github.com/apple/foundationdb/releases/download/7.3.63/foundationdb-clients_7.3.63-1_amd64.deb \
    && dpkg -i /tmp/fdb-clients.deb \
    && rm /tmp/fdb-clients.deb \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /usr/src/cask

COPY . .

RUN go mod download && \
  go mod verify

RUN go build \
    -trimpath \
    -tags timetzdata \
    -o /cask-bin \
    ./cmd/cask

FROM debian:bookworm-slim

ENV DEBIAN_FRONTEND="noninteractive"
ENV TZ=Etc/UTC
ENV GODEBUG="netdns=go"

RUN apt-get update && apt-get install --yes \
  dumb-init \
  ca-certificates \
  runit

WORKDIR /cask
COPY --from=build-env /cask-bin /usr/bin/cask

ENTRYPOINT ["/usr/bin/dumb-init", "--"]
CMD ["/usr/bin/cask"]

LABEL org.opencontainers.image.source=https://github.com/bluesky-social/indigo
LABEL org.opencontainers.image.description="cask high-availability atproto firehose fan-out service"
LABEL org.opencontainers.image.licenses="MIT OR Apache-2.0"
