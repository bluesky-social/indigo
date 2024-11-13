FROM golang:1.23.3-alpine3.19 AS custom-go
RUN apk add --no-cache build-base make git

# Clone go source
RUN git clone https://go.googlesource.com/go /gosource

WORKDIR /gosource

# Checkout go version 1.23.3
RUN git checkout go1.23.3

# Cherry pick the CL
RUN git fetch https://go.googlesource.com/go refs/changes/97/564197/1 && git cherry-pick FETCH_HEAD

# Build go
RUN ./src/all.bash

# Copy the artifacts to /export
RUN mkdir -p /export
RUN cp /gosource/bin/go /export/go
