# Run this dockerfile from the top level of the indigo git repository like:
#
#   podman build -f ./cmd/hepa/Dockerfile -t hepa .

### Compile stage
FROM golang:1.23-alpine3.20 AS build-env
RUN apk add --no-cache build-base make git

ADD . /dockerbuild
WORKDIR /dockerbuild

# timezone data for alpine builds
ENV GOEXPERIMENT=loopvar
RUN GIT_VERSION=$(git describe --tags --long --always) && \
    go build -tags timetzdata -o /hepa ./cmd/hepa

### Run stage
FROM alpine:3.20

RUN apk add --no-cache --update dumb-init ca-certificates
ENTRYPOINT ["dumb-init", "--"]

WORKDIR /
RUN mkdir -p data/hepa
COPY --from=build-env /hepa /

# small things to make golang binaries work well under alpine
ENV GODEBUG=netdns=go
ENV TZ=Etc/UTC

EXPOSE 2210

CMD ["/hepa", "run"]

LABEL org.opencontainers.image.source=https://github.com/bluesky-social/indigo
LABEL org.opencontainers.image.description="atproto Auto-Moderation Service (hepa, indigo edition)"
LABEL org.opencontainers.image.licenses=MIT
