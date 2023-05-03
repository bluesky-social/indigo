#
# Build container.
#
FROM golang:1.20-bullseye AS build-env

WORKDIR /usr/src/thecloud

ENV DEBIAN_FRONTEND=noninteractive

# Go settings.
ENV GODEBUG=netdns=go
ENV GOOS=linux
ENV GOARCH=amd64
ENV CGO_ENABLED=1

# Copy files in.
COPY . .

# Generate the Go binary.
RUN go mod download && go mod verify
RUN go build \
    -v  \
    -trimpath \
    -tags timetzdata \
    -o /thecloud \
    ./

#
# Run container.
#
FROM debian:bullseye-slim

ENV GODEBUG=netdns=go
ENV TZ=Etc/UTC
ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install --yes \
  dumb-init \
  ca-certificates

ENTRYPOINT ["dumb-init", "--"]

WORKDIR /
COPY --from=build-env /thecloud /usr/bin/thecloud

CMD ["/usr/bin/thecloud"]

LABEL org.opencontainers.image.source=https://github.com/bluesky-social/thecloud
LABEL org.opencontainers.image.description="bsky.app Search"
LABEL org.opencontainers.image.licenses=UNKNOWN
