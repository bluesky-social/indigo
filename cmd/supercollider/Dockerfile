# Stage 1: Build the Go binary
FROM golang:1.23-alpine3.20 AS builder

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
COPY --from=builder /supercollider /supercollider

# Set the startup command to run the binary
CMD ["/supercollider"]
