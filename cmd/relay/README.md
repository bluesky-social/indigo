
`relay`: atproto relay reference implementation
===============================================

*NOTE: "relays" used to be called "Big Graph Servers", or "BGS", or "bigsky". Many variables and packages still reference "bgs"*

This is a reference implementation of an atproto relay, written and operated by Bluesky.

In [atproto](https://atproto.com), a relay subscribes to multiple PDS hosts and outputs a combined "firehose" event stream. Downstream services can subscribe to this single firehose a get all relevant events for the entire network, or a specific sub-graph of the network. The relay verifies repo data structure integrity and identity signatures. It is application-agnostic, and does not validate data records against atproto Lexicon schemas.

This relay implementation is designed to subscribe to the entire global network. The current state of the codebase is informally expected to scale to around 100 million accounts in the network, and tens of thousands of repo events per second (peak).

Features and design decisions:

- runs on a single server (not a distributed system)
- upstream host and account state is stored in a SQL database
- SQL driver: [gorm](https://gorm.io), supporting PostgreSQL in production and sqlite for testing
- highly concurrent: not particularly CPU intensive
- single golang binary for easy deployment
- observability: logging, prometheus metrics, OTEL traces
- admin web interface: configure limits, add upstream PDS instances, etc

This daemon is relatively simple to self-host, though it isn't as well documented or supported as the PDS reference implementation (see details below).

See `./HACKING.md` for more documentation of specific behaviors of this implementation.


## Development Tips

The README and Makefile at the top level of this git repo have some generic helpers for testing, linting, formatting code, etc.

To build the admin web interface, and then build and run the relay locally:

    make build-relay-admin-ui
    make run-dev-relay

You can run the command directly to get a list of configuration flags and environment variables. The environment will be loaded from a `.env`file if one exist:

    go run ./cmd/relay/ --help

You can also build an run the command directly:

    go build ./cmd/relay
    ./relay serve

By default, the daemon will use sqlite for databases (in the directory `./data/relay/`), and the HTTP API will be bound to localhost port 2470.

When the daemon isn't running, sqlite database files can be inspected with:

    sqlite3 data/relay/relay.sqlite
    [...]
    sqlite> .schema

To wipe all local data (careful!):

    # double-check before running this destructive command
    rm -rf ./data/relay/*

There is a basic web dashboard, though it will not be included unless built and copied to a local directory `./public/`. Run `make build-relay-admin-ui`, and then when running the daemon the dashboard will be available at: <http://localhost:2470/dash/>. Paste in the admin key, eg `dummy`.

The local admin routes can also be accessed by passing the admin password using HTTP Basic auth (with username `admin`), for example:

    http get :2470/admin/pds/list -a admin:dummy

Request crawl of an individual PDS instance like:

    http post :2470/admin/pds/requestCrawl -a admin:dummy hostname=pds.example.com

The `goat` command line tool (also part of the indigo git repository) includes helpers for administering, inspecting, and debugging relays:

    RELAY_HOST=http://localhost:2470 goat firehose --verify-mst
    RELAY_HOST=http://localhost:2470 goat relay admin host list

## API Endpoints

This relay implements the core atproto "sync" API endpoints:

- `GET /xrpc/com.atproto.sync.subscribeRepos` (WebSocket)
- `GET /xrpc/com.atproto.sync.getRepo` (HTTP redirect to account's PDS)
- `GET /xrpc/com.atproto.sync.getRepoStatus`
- `GET /xrpc/com.atproto.sync.listRepos` (optional)
- `GET /xrpc/com.atproto.sync.getLatestCommit` (optional)

It also implements some relay-specific endpoints:

- `POST /xrpc/com.atproto.sync.requestCrawl`
- `GET /xrpc/com.atproto.sync.listHosts`
- `GET /xrpc/com.atproto.sync.getHostStatus`

Documentation can be found in the [atproto specifications](https://atproto.com/specs/sync) for repository synchronization, event streams, data formats, account status, etc.

This implementation also has some off-protocol admin endpoints under `/admin/`. These have legacy schemas from an earlier implementation, are not well documented, and should not be considered a stable API to build upon. The intention is to refactor them in to Lexicon-specified APIs.

## Configuration and Operation

*NOTE: this document is not a complete guide to operating a relay as a public service. That requires planning around acceptable use policies, financial sustainability, infrastructure selection, etc. This is just a quick overview of the mechanics of getting a relay up and running.*

Some notable configuration env vars:

- `RELAY_ADMIN_PASSWORD`
- `DATABASE_URL`: eg, `postgres://relay:CHANGEME@localhost:5432/relay`
- `RELAY_PERSIST_DIR`: storage location for "backfill" events, eg `/data/relay/persist`
- `RELAY_REPLAY_WINDOW`: the duration of output "backfill window", eg `24h`
- `RELAY_LENIENT_SYNC_VALIDATION`: if `true`, allow legacy upstreams which don't implement atproto sync v1.1
- `RELAY_TRUSTED_DOMAINS`: patterns of PDS hosts which get larger quotas by default, eg `*.host.bsky.network`

There is a health check endpoint at `/xrpc/_health`. Prometheus metrics are exposed by default on port 2471, path `/metrics`. The service logs fairly verbosely to stdout; use `LOG_LEVEL` to control log volume (`warn`, `info`, etc).

Be sure to double-check bandwidth usage and pricing if running a public relay! Bandwidth prices can vary widely between providers, and popular cloud services (AWS, Google Cloud, Azure) are very expensive compared to alternatives like OVH or Hetzner.

The relay admin interface has flexibility for many situations, but in some operational incidents it may be necessary to run SQL commands to do cleanups. This should be done when the relay is not actively operating. It is also recommended to run SQL commands in a transaction that can be rolled back in case of a typo or mistake.

On the public web, you should probably run the relay behind a load-balancer or reverse proxy like `haproxy` or `caddy`, which manages TLS and can have various HTTP limits and behaviors configured. Remember that WebSocket support is required.

The relay does not resolve atproto handles, but it does do DNS resolutions for hostnames, and may do a burst of resolutions at startup. Note that the go runtime may have an internal DNS implementation enabled (this is the default for the Dockerfile). The relay *will* do a large number of DID resolutions, particularly calls to the PLC directory, and particularly after a process restart when the in-process identity cache is warming up.

### PostgreSQL

PostgreSQL is recommended for any non-trival relay deployments. Database configuration is passed via the `DATABASE_URL` environment variable, or the corresponding CLI arg.

The user and database must already be configured. For example:

    CREATE DATABASE relay;

    CREATE USER ${username} WITH PASSWORD '${password}';
    GRANT ALL PRIVILEGES ON DATABASE relay TO ${username};

This service currently uses `gorm` to automatically run database migrations as the regular user. There is no support for running database migrations separately under more privileged database user.

### Docker

The relay is relatively easy to build and operate as as simple executable, but there is also Dockerfile in this directory. It can be used to build customized/patched versions of the relay as a container, republish them, run locally, deploy to servers, deploy to an orchestrated cluster, etc.

We strongly recommend running docker in "host networking" mode when operating a full-network relay. You may also want to use something other than default docker log management (eg, `svlogd`).

### Bootstrapping Host List

Before bulk-adding hosts, you should probably increase the "new-hosts-per-day" limit, at least temporarily.

The relay comes with a helper command to pull a list of hosts from an existing relay. You should shut the relay down first and run this as a separate command:

    ./relay pull-hosts

An alternative method, using `goat` and `parallel`, which is more gentle and may be better for small servers:

    # dump a host list using goat
    # 'rg' is ripgrep
    RELAY_HOST=https://relay1.us-west.bsky.network goat relay host list | rg '\tactive' | cut -f1 > hosts.txt

    # assuming that .env contains local relay configuration and admin credential
    shuf hosts.txt | parallel goat relay admin host add {}
