# Stage 1: Build the Go binary
FROM golang:1.24-alpine3.22 AS build-env

# Create a directory for the application
WORKDIR /app

# Fetch dependencies
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build the application
ENV GOEXPERIMENT=loopvar
RUN CGO_ENABLED=1 GOOS=linux go build -o /supercollider ./cmd/supercollider

FROM alpine:latest as certs

RUN apk --update add ca-certificates

FROM debian:stable-slim

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

# Copy the binary from the first stage.
COPY --from=build-env /supercollider /supercollider

# Set the startup command to run the binary
CMD ["/supercollider"]

LABEL org.opencontainers.image.source=https://github.com/bluesky-social/indigo
LABEL org.opencontainers.image.description="AT Proto (supercollider)"
LABEL org.opencontainers.image.licenses="MIT OR Apache-2.0"
