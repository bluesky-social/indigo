
`rainbow`: atproto Firehose Fanout Service
==========================================

This is an atproto service which consumes from a firehose (eg, from a relay or PDS) and fans out events to many subscribers.

Features and design points:

- retains "backfill window" on local disk (using [pebble](https://github.com/cockroachdb/pebble))
- serves the `com.atproto.sync.subscribeRepos` endpoint (WebSocket)
- retains upstream firehose "sequence numbers"
- does not validate events (signatures, repo tree, hashes, etc), just passes through
- does not archive or mirror individual records or entire repositories (or implement related API endpoints)
- disk I/O intensive: fast NVMe disks are recommended, and RAM is helpful for caching
- single golang binary for easy deployment
- observability: logging, prometheus metrics, OTEL traces

## Running 

This is a simple, single-binary Go program. You can also build and run it as a docker container (see `./Dockerfile`).

From the top level of this repo, you can build:

```shell
go build ./cmd/rainbow -o rainbow-bin
```

or just run it, and see configuration options:

```shell
go run ./cmd/rainbow --help
```
