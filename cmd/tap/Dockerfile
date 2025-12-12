# Run this dockerfile from the top level of the indigo git repository like:
#
#   podman build -f ./cmd/tap/Dockerfile -t tap .

### Compile stage
FROM golang:1.25-alpine3.22 AS build-env
RUN apk add --no-cache build-base make git

ADD . /dockerbuild
WORKDIR /dockerbuild

# timezone data for alpine builds
RUN GIT_VERSION=$(git describe --tags --long --always) && \
    go build -tags timetzdata -o /tap ./cmd/tap

### Run stage
FROM alpine:3.22

RUN apk add --no-cache --update dumb-init ca-certificates runit
ENTRYPOINT ["dumb-init", "--"]

RUN mkdir -p /data
WORKDIR /data
COPY --from=build-env /tap /

# small things to make golang binaries work well under alpine
ENV GODEBUG=netdns=go
ENV TZ=Etc/UTC

EXPOSE 2480

CMD ["/tap", "run"]

LABEL org.opencontainers.image.source=https://github.com/bluesky-social/indigo
LABEL org.opencontainers.image.description="AT Protocol Sync Utility (tap)"
LABEL org.opencontainers.image.licenses=MIT
