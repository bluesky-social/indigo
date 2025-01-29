FROM golang:1.23-bullseye AS build-env

ENV DEBIAN_FRONTEND=noninteractive
ENV TZ=Etc/UTC
ENV GODEBUG="netdns=go"
ENV GOOS="linux"
ENV GOARCH="amd64"
ENV CGO_ENABLED="1"

WORKDIR /usr/src/collectiondir

COPY . .

RUN go mod download && \
  go mod verify

RUN go build \
    -v  \
    -trimpath \
    -tags timetzdata \
    -o /collectiondir-bin \
    ./cmd/collectiondir

FROM debian:bullseye-slim

ENV DEBIAN_FRONTEND="noninteractive"
ENV TZ=Etc/UTC
ENV GODEBUG="netdns=go"

RUN apt-get update && apt-get install --yes \
  dumb-init \
  ca-certificates \
  runit

WORKDIR /collectiondir
COPY --from=build-env /collectiondir-bin /usr/bin/collectiondir

ENTRYPOINT ["/usr/bin/dumb-init", "--"]
CMD ["/usr/bin/collectiondir"]

LABEL org.opencontainers.image.source=https://github.com/bluesky-social/indigo
LABEL org.opencontainers.image.description="collectiondir "
LABEL org.opencontainers.image.licenses=MIT
