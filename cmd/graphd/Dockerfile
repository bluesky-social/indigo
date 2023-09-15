# Stage 1: Build the Go binary
FROM golang:1.21-alpine3.18 AS build-env
RUN apk add --no-cache build-base make git

# Create a directory for the application
WORKDIR /app

# Fetch dependencies
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build the application
# timezone data for alpine builds
ENV GOEXPERIMENT=loopvar
RUN GIT_VERSION=$(git describe --tags --long --always) && \
    go build -tags timetzdata -ldflags="-X github.com/bluesky-social/indigo/version.Version=$GIT_VERSION"  -o /graphd ./cmd/graphd

### Run stage
FROM alpine:3.18

RUN apk add --no-cache --update dumb-init ca-certificates
ENTRYPOINT ["dumb-init", "--"]

WORKDIR /
RUN mkdir -p data/graphd

# Copy the binary from the first stage.
COPY --from=build-env /graphd /graphd

# small things to make golang binaries work well under alpine
ENV GODEBUG=netdns=go
ENV TZ=Etc/UTC

# Set the startup command to run the binary
CMD ["/graphd"]
