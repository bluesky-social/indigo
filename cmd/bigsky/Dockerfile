# Run this dockerfile from the top level of the indigo git repository like:
#
#   podman build -f ./cmd/bigsky/Dockerfile -t bigsky .

FROM golang:1.23.3-bookworm AS custom-go
RUN apt update && apt install -y build-essential make git

# Git config
RUN git config --global user.email "automated_action_runner@github"
RUN git config --global user.name "automated_action_runner"

# Clone go source
RUN git clone https://go.googlesource.com/go /gosource

WORKDIR /gosource

# Checkout go version 1.23.3
RUN git checkout go1.23.3

# Cherry pick the CL
RUN git fetch https://go.googlesource.com/go refs/changes/97/564197/1 && git cherry-pick FETCH_HEAD

WORKDIR /gosource/src

# Build go
RUN ./all.bash

# Copy the artifacts to /export
RUN mkdir -p /export
RUN cp /gosource/bin/go /export/go


### Compile stage
FROM custom-go AS build-env

ADD . /dockerbuild
WORKDIR /dockerbuild

# timezone data for alpine builds
ENV GOEXPERIMENT=loopvar
RUN GIT_VERSION=$(git describe --tags --long --always) && \
    /export/go build -tags timetzdata -o /bigsky ./cmd/bigsky

### Build Frontend stage
FROM node:18-alpine AS web-builder

WORKDIR /app

COPY ts/bgs-dash /app/

RUN yarn install --frozen-lockfile

RUN yarn build

### Run stage
FROM alpine:3.19

RUN apk add --no-cache --update dumb-init ca-certificates runit
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
LABEL org.opencontainers.image.description="ATP Relay (aka BGS)"
LABEL org.opencontainers.image.licenses=MIT
