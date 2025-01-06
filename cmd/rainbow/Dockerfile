FROM golang:1.23-bullseye AS build-env

ENV DEBIAN_FRONTEND=noninteractive
ENV TZ=Etc/UTC
ENV GODEBUG="netdns=go"
ENV GOOS="linux"
ENV GOARCH="amd64"
ENV CGO_ENABLED="1"

WORKDIR /usr/src/rainbow

COPY . .

RUN go mod download && \
  go mod verify

RUN go build \
    -v  \
    -trimpath \
    -tags timetzdata \
    -o /rainbow-bin \
    ./cmd/rainbow

FROM debian:bullseye-slim

ENV DEBIAN_FRONTEND="noninteractive"
ENV TZ=Etc/UTC
ENV GODEBUG="netdns=go"

RUN apt-get update && apt-get install --yes \
  dumb-init \
  ca-certificates \
  runit

WORKDIR /rainbow
COPY --from=build-env /rainbow-bin /usr/bin/rainbow

ENTRYPOINT ["/usr/bin/dumb-init", "--"]
CMD ["/usr/bin/rainbow"]

LABEL org.opencontainers.image.source=https://github.com/bluesky-social/indigo
LABEL org.opencontainers.image.description="rainbow atproto firehose fanout service"
LABEL org.opencontainers.image.licenses=MIT
