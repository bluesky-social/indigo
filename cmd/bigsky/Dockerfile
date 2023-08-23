# Run this dockerfile from the top level of the indigo git repository like:
#
#   podman build -f ./cmd/bigsky/Dockerfile -t bigsky .

### Compile stage
FROM golang:1.21-alpine3.18 AS build-env
RUN apk add --no-cache build-base make git

ADD . /dockerbuild
WORKDIR /dockerbuild

# timezone data for alpine builds
RUN GIT_VERSION=$(git describe --tags --long --always) && \
    go build -tags timetzdata -ldflags="-X github.com/bluesky-social/indigo/version.Version=$GIT_VERSION"  -o /bigsky ./cmd/bigsky

### Build Frontend stage
FROM node:18-alpine as web-builder

WORKDIR /app

COPY ts/bgs-dash /app/

RUN yarn install --frozen-lockfile

RUN yarn build

### Run stage
FROM alpine:3.18

RUN apk add --no-cache --update dumb-init ca-certificates
ENTRYPOINT ["dumb-init", "--"]

WORKDIR /
RUN mkdir -p data/bigsky
COPY --from=build-env /bigsky /
COPY --from=web-builder /app/dist/ public/

# small things to make golang binaries work well under alpine
ENV GODEBUG=netdns=go
ENV TZ=Etc/UTC

EXPOSE 2470

CMD ["/bigsky"]

LABEL org.opencontainers.image.source=https://github.com/bluesky-social/indigo
LABEL org.opencontainers.image.description="ATP Big Graph Server (BGS)"
LABEL org.opencontainers.image.licenses=MIT
