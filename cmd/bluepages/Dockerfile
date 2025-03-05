# Run this dockerfile from the top level of the indigo git repository like:
#
#   podman build -f ./cmd/bluepages/Dockerfile -t bluepages .

### Compile stage
FROM golang:1.23-alpine3.20 AS build-env
RUN apk add --no-cache build-base make git

ADD . /dockerbuild
WORKDIR /dockerbuild

# timezone data for alpine builds
ENV GOEXPERIMENT=loopvar
RUN GIT_VERSION=$(git describe --tags --long --always) && \
    go build -tags timetzdata -o /bluepages ./cmd/bluepages

### Run stage
FROM alpine:3.20

RUN apk add --no-cache --update dumb-init ca-certificates runit

WORKDIR /
RUN mkdir -p data/bluepages
COPY --from=build-env /bluepages /

# small things to make golang binaries work well under alpine
ENV GODEBUG=netdns=go
ENV TZ=Etc/UTC

EXPOSE 6600

ENTRYPOINT ["/usr/bin/dumb-init", "--"]
CMD ["/bluepages", "serve"]

LABEL org.opencontainers.image.source=https://github.com/bluesky-social/indigo
LABEL org.opencontainers.image.description="atproto identity directory (bluepages)"
LABEL org.opencontainers.image.licenses=MIT
