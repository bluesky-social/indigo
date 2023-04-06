# Run this dockerfile from the top level of the indigo git repository like:
#
#   podman build -f ./cmd/labelmaker/Dockerfile -t labelmaker .

### Compile stage
FROM golang:1.20-alpine3.17 AS build-env
RUN apk add --no-cache build-base make git

ADD . /dockerbuild
WORKDIR /dockerbuild

# timezone data for alpine builds
RUN GIT_VERSION=$(git describe --tags --long --always) && \
    go build -tags timetzdata -ldflags="-X github.com/bluesky-social/indigo/version.Version=$GIT_VERSION" -o /labelmaker ./cmd/labelmaker

### Run stage
FROM alpine:3.17

RUN apk add --no-cache --update dumb-init ca-certificates
ENTRYPOINT ["dumb-init", "--"]

WORKDIR /
RUN mkdir -p data/labelmaker
COPY --from=build-env /labelmaker /

# small things to make golang binaries work well under alpine
ENV GODEBUG=netdns=go
ENV TZ=Etc/UTC

EXPOSE 2470

CMD ["/labelmaker"]

LABEL org.opencontainers.image.source=https://github.com/bluesky-social/indigo
LABEL org.opencontainers.image.description="ATP Labeling Service (labelmaker)"
LABEL org.opencontainers.image.licenses=MIT
