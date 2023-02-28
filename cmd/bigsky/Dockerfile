# Run this dockerfile from the top level of the indigo git repository like:
#
#   podman build -f ./cmd/bigsky/Dockerfile -t bigsky .

### Compile stage

# trouble with 'No such file or directory' when building using 'golang:1.19'
# images, I think because of library errors when copied to 'alpine' image below
# https://megamorf.gitlab.io/2019/09/08/alpine-go-builds-with-cgo-enabled/
# TODO: lock to a specific version of go toolchain
FROM alpine:3.17 AS build-env
RUN apk add --no-cache build-base make go

ADD . /dockerbuild
WORKDIR /dockerbuild

# timezone data for alpine builds
RUN go build -tags timetzdata -o /bigsky ./cmd/bigsky

### Run stage
FROM alpine:3.17

RUN apk add --no-cache --update dumb-init ca-certificates
ENTRYPOINT ["dumb-init", "--"]

WORKDIR /
RUN mkdir -p data/bigsky
COPY --from=build-env /bigsky /

# small things to make golang binaries work well under alpine
ENV GODEBUG=netdns=go
ENV TZ=Etc/UTC

EXPOSE 2470

CMD ["/bigsky"]

LABEL org.opencontainers.image.source=https://github.com/bluesky-social/indigo
LABEL org.opencontainers.image.description="ATP Big Graph Server (BGS)"
LABEL org.opencontainers.image.licenses=MIT
