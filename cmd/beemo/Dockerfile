# Run this dockerfile from the top level of the indigo git repository like:
#
#   podman build -f ./cmd/beemo/Dockerfile -t beemo .

### Compile stage
FROM alpine:3.17 AS build-env
RUN apk add --no-cache build-base make go git

ADD . /dockerbuild
WORKDIR /dockerbuild

# timezone data for alpine builds
RUN GIT_VERSION=$(git describe --tags --long --always) && \
    go build -tags timetzdata -ldflags="-X github.com/bluesky-social/indigo/version.Version=$GIT_VERSION" -o /beemo ./cmd/beemo

### Run stage
FROM alpine:3.17

RUN apk add --no-cache --update dumb-init ca-certificates
ENTRYPOINT ["dumb-init", "--"]

WORKDIR /
RUN mkdir -p data/beemo
COPY --from=build-env /beemo /

# small things to make golang binaries work well under alpine
ENV GODEBUG=netdns=go
ENV TZ=Etc/UTC

CMD ["/beemo"]

LABEL org.opencontainers.image.source=https://github.com/bluesky-social/indigo
LABEL org.opencontainers.image.description="ATP ChatOps Bot (beemo)"
LABEL org.opencontainers.image.licenses=MIT
