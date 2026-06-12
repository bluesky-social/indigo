#!/usr/bin/env sh

find cmd/relay/ | entr -r sh -c "echo \"[$(date +%T)] Running relay\" && dlv debug --headless --listen=0.0.0.0:12470 --api-version=2 --accept-multiclient --continue ./cmd/relay -- serve"
