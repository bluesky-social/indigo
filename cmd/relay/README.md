
`relay`: atproto relay reference implementation
===============================================

*NOTE: "relays" used to be called "Big Graph Servers", or "BGS", or "bigsky". Many variables and packages still reference "bgs"*

This is a reference implementation of an atproto relay, written and operated by Bluesky.

In atproto, a relay subscribes to multiple PDS hosts and outputs a combined "firehose" event stream. Downstream services can subscribe to this single firehose a get all relevant events for the entire network, or a specific sub-graph of the network. The relay maintains a mirror of repo data from all accounts on the upstream PDS instances, and verifies repo data structure integrity and identity signatures. It is agnostic to applications, and does not validate data against atproto Lexicon schemas.

This relay implementation is designed to subscribe to the entire global network. The current state of the codebase is informally expected to scale to around 100 million accounts in the network, and tens of thousands of repo events per second (peak).

Features and design decisions:

- runs on a single server
- crawling and account state: stored in SQL database
- SQL driver: gorm, with PostgreSQL in production and sqlite for testing
- highly concurrent: not particularly CPU intensive
- single golang binary for easy deployment
- observability: logging, prometheus metrics, OTEL traces
- admin web interface: configure limits, add upstream PDS instances, etc

This software is not yet as packaged, documented, and supported for self-hosting as our PDS distribution or Ozone service. But it is relatively simple and inexpensive to get running.

A note and reminder about relays in general are that they are more of a convenience in the protocol than a hard requirement. The "firehose" API is the exact same on the PDS and on a relay. Any service which subscribes to the relay could instead connect to one or more PDS instances directly.


## Development Tips

The README and Makefile at the top level of this git repo have some generic helpers for testing, linting, formatting code, etc.

To re-build and run the relay locally:

    make run-dev-relay

You can re-build and run the command directly to get a list of configuration flags and env vars; env vars will be loaded from `.env` if that file exists:

    RELAY_ADMIN_PASSWORD=dummy go run ./cmd/relay/ --help

By default, the daemon will use sqlite for databases (in the directory `./data/relay/`) and the HTTP API will be bound to localhost port 2470.

When the daemon isn't running, sqlite database files can be inspected with:

    sqlite3 data/relay/relay.sqlite
    [...]
    sqlite> .schema

Wipe all local data:

    # careful! double-check this destructive command
    rm -rf ./data/relay/*

There is a basic web dashboard, though it will not be included unless built and copied to a local directory `./public/`. Run `make build-relay-ui`, and then when running the daemon the dashboard will be available at: <http://localhost:2470/dash/>. Paste in the admin key, eg `dummy`.

The local admin routes can also be accessed by passing the admin password using HTTP Basic auth (with username `admin`), for example:

    http get :2470/admin/pds/list -a admin:dummy

Request crawl of an individual PDS instance like:

    http post :2470/admin/pds/requestCrawl -a admin:dummy hostname=pds.example.com


## Docker Containers

One way to deploy is running a docker image. You can pull and/or run a specific version of relay, referenced by git commit, from the Bluesky Github container registry. For example:

    docker pull ghcr.io/bluesky-social/indigo:relay-fd66f93ce1412a3678a1dd3e6d53320b725978a6
    docker run ghcr.io/bluesky-social/indigo:relay-fd66f93ce1412a3678a1dd3e6d53320b725978a6

There is a Dockerfile in this directory, which can be used to build customized/patched versions of the relay as a container, republish them, run locally, deploy to servers, deploy to an orchestrated cluster, etc. See docs and guides for docker and cluster management systems for details.


## Database Setup

PostgreSQL and Sqlite are both supported. Database configuration is passed via the `DATABASE_URL` environment variable, or the corresponding CLI arg.

For PostgreSQL, the user and database must already be configured. Some example SQL commands are:

    CREATE DATABASE relay;

    CREATE USER ${username} WITH PASSWORD '${password}';
    GRANT ALL PRIVILEGES ON DATABASE relay TO ${username};

This service currently uses `gorm` to automatically run database migrations as the regular user. There is no concept of running a separate set of migrations under more privileged database user.


## Deployment

*NOTE: this is not a complete guide to operating a relay. There are decisions to be made and communicated about policies, bandwidth use, PDS crawling and rate-limits, financial sustainability, etc, which are not covered here. This is just a quick overview of how to technically get a relay up and running.*

In a real-world system, you will probably want to use PostgreSQL.

Some notable configuration env vars to set:

- `RELAY_ADMIN_PASSWORD`
- `DATABASE_URL`: eg, `postgres://relay:CHANGEME@localhost:5432/relay`
- `RELAY_PERSIST_DIR`: storage location for "backfill" events, eg `/data/relay/persist`
- `RELAY_REPLAY_WINDOW`: the duration of output "backfill window", eg `24h`
- `RELAY_LENIENT_SYNC_VALIDATION`: if `true`, allow legacy upstreams which don't implement atproto sync v1.1
- `RELAY_TRUSTED_DOMAINS`: patterns of PDS hosts which get larger quotas by default, eg `*.host.bsky.network`

There is a health check endpoint at `/xrpc/_health`. Prometheus metrics are exposed by default on port 2471, path `/metrics`. The service logs fairly verbosely to stdout; use `LOG_LEVEL` to control log volume (`warn`, `info`, etc).

Be sure to double-check bandwidth usage and pricing if running a public relay! Bandwidth prices can vary widely between providers, and popular cloud services (AWS, Google Cloud, Azure) are very expensive compared to alternatives like OVH or Hetzner.
