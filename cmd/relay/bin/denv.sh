#!/usr/bin/env sh

find cmd/relay/ | entr -r go run ./cmd/relay serve
