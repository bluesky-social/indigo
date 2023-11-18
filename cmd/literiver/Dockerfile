### Compile stage
FROM golang:1.21-alpine3.18 AS build-env
RUN apk add --no-cache build-base make git

ADD . /dockerbuild
WORKDIR /dockerbuild

# timezone data for alpine builds
ENV GOEXPERIMENT=loopvar
RUN GIT_VERSION=$(git describe --tags --long --always) && \
    go build -ldflags "-extldflags '-static'" -tags osusergo,netgo,sqlite_omit_load_extension,timetzdata -o /literiver ./cmd/literiver

### Run stage
FROM alpine:3.18

RUN apk add --no-cache --update dumb-init ca-certificates
ENTRYPOINT ["dumb-init", "--"]

WORKDIR /
RUN mkdir -p data/literiver
COPY --from=build-env /literiver /

# small things to make golang binaries work well under alpine
ENV GODEBUG=netdns=go
ENV TZ=Etc/UTC

EXPOSE 9032

CMD ["/literiver"]

LABEL org.opencontainers.image.source=https://github.com/bluesky-social/literiver
LABEL org.opencontainers.image.description="Litestream-based Mass SQLite Replicator"
LABEL org.opencontainers.image.licenses=MIT
