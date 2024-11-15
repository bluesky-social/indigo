
`bigsky`: atproto Relay Service
===============================

*NOTE: "Relays" used to be called "Big Graph Servers", or "BGS", which inspired the name "bigsky". Many variables and packages still reference "bgs"*

This is the implementation of an atproto Relay which is running in the production network, written and operated by Bluesky.

In atproto, a Relay subscribes to multiple PDS hosts and outputs a combined "firehose" event stream. Downstream services can subscribe to this single firehose a get all relevant events for the entire network, or a specific sub-graph of the network. The Relay maintains a mirror of repo data from all accounts on the upstream PDS instances, and verifies repo data structure integrity and identity signatures. It is agnostic to applications, and does not validate data against atproto Lexicon schemas.

This Relay implementation is designed to subscribe to the entire global network. The current state of the codebase is informally expected to scale to around 20 million accounts in the network, and thousands of repo events per second (peak).

Features and design decisions:

- runs on a single server
- repo data: stored on-disk in individual CAR "slice" files, with metadata in SQL. filesystem must accommodate tens of millions of small files
- firehose backfill data: stored on-disk by default, with metadata in SQL
- crawling and account state: stored in SQL database
- SQL driver: gorm, with PostgreSQL in production and sqlite for testing
- disk I/O intensive: fast NVMe disks are recommended, and RAM is helpful for caching
- highly concurrent: not particularly CPU intensive
- single golang binary for easy deployment
- observability: logging, prometheus metrics, OTEL traces
- "spidering" feature to auto-discover new accounts (DIDs)
- ability to export/import lists of DIDs to "backfill" Relay instances
- periodic repo compaction
- admin web interface: configure limits, add upstream PDS instances, etc

This software is not as packaged, documented, and supported for self-hosting as our PDS distribution or Ozone service. But it is relatively simple and inexpensive to get running.

A note and reminder about Relays in general are that they are more of a convenience in the protocol than a hard requirement. The "firehose" API is the exact same on the PDS and on a Relay. Any service which subscribes to the Relay could instead connect to one or more PDS instances directly.


## Development Tips

The README and Makefile at the top level of this git repo have some generic helpers for testing, linting, formatting code, etc.

To re-build and run the bigsky Relay locally:

    make run-dev-relay

You can re-build and run the command directly to get a list of configuration flags and env vars; env vars will be loaded from `.env` if that file exists:

    RELAY_ADMIN_KEY=localdev go run ./cmd/bigsky/ --help

By default, the daemon will use sqlite for databases (in the directory `./data/bigsky/`), CAR data will be stored as individual shard files in `./data/bigsky/carstore/`), and the HTTP API will be bound to localhost port 2470.

When the daemon isn't running, sqlite database files can be inspected with:

    sqlite3 data/bigsky/bgs.sqlite
    [...]
    sqlite> .schema

Wipe all local data:

    # careful! double-check this destructive command
    rm -rf ./data/bigsky/*

There is a basic web dashboard, though it will not be included unless built and copied to a local directory `./public/`. Run `make build-relay-ui`, and then when running the daemon the dashboard will be available at: <http://localhost:2470/dash/>. Paste in the admin key, eg `localdev`.

The local admin routes can also be accessed by passing the admin key as a bearer token, for example:

    http get :2470/admin/pds/list Authorization:"Bearer localdev"

Request crawl of an individual PDS instance like:

    http post :2470/admin/pds/requestCrawl Authorization:"Bearer localdev" hostname=pds.example.com


## Docker Containers

One way to deploy is running a docker image. You can pull and/or run a specific version of bigsky, referenced by git commit, from the Bluesky Github container registry. For example:

    docker pull ghcr.io/bluesky-social/indigo:bigsky-fd66f93ce1412a3678a1dd3e6d53320b725978a6
    docker run ghcr.io/bluesky-social/indigo:bigsky-fd66f93ce1412a3678a1dd3e6d53320b725978a6

There is a Dockerfile in this directory, which can be used to build customized/patched versions of the Relay as a container, republish them, run locally, deploy to servers, deploy to an orchestrated cluster, etc. See docs and guides for docker and cluster management systems for details.


## Database Setup

PostgreSQL and Sqlite are both supported. When using Sqlite, separate files are used for Relay metadata and CarStore metadata. With PostgreSQL a single database server, user, and logical database can all be reused: table names will not conflict.

Database configuration is passed via the `DATABASE_URL` and `CARSTORE_DATABASE_URL` environment variables, or the corresponding CLI args.

For PostgreSQL, the user and database must already be configured. Some example SQL commands are:

    CREATE DATABASE bgs;
    CREATE DATABASE carstore;

    CREATE USER ${username} WITH PASSWORD '${password}';
    GRANT ALL PRIVILEGES ON DATABASE bgs TO ${username};
    GRANT ALL PRIVILEGES ON DATABASE carstore TO ${username};

This service currently uses `gorm` to automatically run database migrations as the regular user. There is no concept of running a separate set of migrations under more privileged database user.


## Deployment

*NOTE: this is not a complete guide to operating a Relay. There are decisions to be made and communicated about policies, bandwidth use, PDS crawling and rate-limits, financial sustainability, etc, which are not covered here. This is just a quick overview of how to technically get a relay up and running.*

In a real-world system, you will probably want to use PostgreSQL for both the relay database and the carstore database. CAR shards will still be stored on-disk, resulting in many millions of files. Chose your storage hardware and filesystem carefully: we recommend XFS on local NVMe, not network-backed blockstorage (eg, not EBS volumes on AWS).

Some notable configuration env vars to set:

- `ENVIRONMENT`: eg, `production`
- `DATABASE_URL`: see section below
- `CARSTORE_DATABASE_URL`: see section below
- `DATA_DIR`: CAR shards will be stored in a subdirectory
- `GOLOG_LOG_LEVEL`: log verbosity
- `RESOLVE_ADDRESS`: DNS server to use
- `FORCE_DNS_UDP`: recommend "true"
- `BGS_COMPACT_INTERVAL`: to control CAR compaction scheduling. for example, "8h" (every 8 hours). Set to "0" to disable automatic compaction.
- `MAX_CARSTORE_CONNECTIONS` and `MAX_METADB_CONNECTIONS`: number of concurrent SQL database connections
- `MAX_FETCH_CONCURRENCY`: how many outbound CAR backfill requests to make in parallel

There is a health check endpoint at `/xrpc/_health`. Prometheus metrics are exposed by default on port 2471, path `/metrics`. The service logs fairly verbosely to stderr; use `GOLOG_LOG_LEVEL` to control log volume.

As a rough guideline for the compute resources needed to run a full-network Relay, in June 2024 an example Relay for over 5 million repositories used:

- around 30 million inodes (files)
- roughly 1 TByte of disk for PostgreSQL
- roughly 1 TByte of disk for CAR shard storage
- roughly 5k disk I/O operations per second (all combined)
- roughly 100% of one CPU core (quite low CPU utilization)
- roughly 5GB of RAM for bigsky, and as much RAM as available for PostgreSQL and page cache
- on the order of 1 megabit inbound bandwidth (crawling PDS instances) and 1 megabit outbound per connected client. 1 mbit continuous is approximately 350 GByte/month

Be sure to double-check bandwidth usage and pricing if running a public relay! Bandwidth prices can vary widely between providers, and popular cloud services (AWS, Google Cloud, Azure) are very expensive compared to alternatives like OVH or Hetzner.


## Bootstrapping the Network

To bootstrap the entire network, you'll want to start with a list of large PDS instances to backfill from. You could pull from a public dashboard of instances (like [mackuba's](https://blue.mackuba.eu/directory/pdses)), or scrape the full DID PLC directory, parse out all PDS service declarations, and sort by count.

Once you have a set of PDS hosts, you can put the bare hostnames (not URLs: no `https://` prefix, port, or path suffix) in a `hosts.txt` file, and then use the `crawl_pds.sh` script to backfill and configure limits for all of them:

    export RELAY_HOST=your.pds.hostname.tld
    export RELAY_ADMIN_KEY=your-secret-key

    # both request crawl, and set generous crawl limits for each
	cat hosts.txt | parallel -j1 ./crawl_pds.sh {}

Just consuming from the firehose for a few hours will only backfill accounts with activity during that period. This is fine to get the backfill process started, but eventually you'll want to do full "resync" of all the repositories on the PDS host to the most recent repo rev version. To enqueue that for all the PDS instances:

    # start sync/backfill of all accounts
	cat hosts.txt | parallel -j1 ./sync_pds.sh {}

Lastly, can monitor progress of any ongoing re-syncs:

    # check sync progress for all hosts
	cat hosts.txt | parallel -j1 ./sync_pds.sh {}


## Admin API

The relay has a number of admin HTTP API endpoints. Given a relay setup listening on port 2470 and with a reasonably secure admin secret:

```
RELAY_ADMIN_PASSWORD=$(openssl rand --hex 16)
bigsky  --api-listen :2470 --admin-key ${RELAY_ADMIN_PASSWORD} ...
```

One can, for example, begin compaction of all repos

```
curl -H 'Authorization: Bearer '${RELAY_ADMIN_PASSWORD} -H 'Content-Type: application/x-www-form-urlencoded' --data '' http://127.0.0.1:2470/admin/repo/compactAll
```

### /admin/subs/getUpstreamConns

Return list of PDS host names in json array of strings: ["host", ...]

### /admin/subs/perDayLimit

Return `{"limit": int}` for the number of new PDS subscriptions that the relay may start in a rolling 24 hour window.

### /admin/subs/setPerDayLimit

POST with `?limit={int}` to set the number of new PDS subscriptions that the relay may start in a rolling 24 hour window.

### /admin/subs/setEnabled

POST with param `?enabled=true` or `?enabled=false` to enable or disable PDS-requested new-PDS crawling.

### /admin/subs/getEnabled

Return `{"enabled": bool}` if non-admin new PDS crawl requests are enabled

### /admin/subs/killUpstream

POST with `?host={pds host name}` to disconnect from their firehose.

Optionally add `&block=true` to prevent connecting to them in the future.

### /admin/subs/listDomainBans

Return `{"banned_domains": ["host name", ...]}`

### /admin/subs/banDomain

POST `{"Domain": "host name"}` to ban a domain

### /admin/subs/unbanDomain

POST `{"Domain": "host name"}` to un-ban a domain

### /admin/repo/takeDown

POST `{"did": "did:..."}` to take-down a bad repo; deletes all local data for the repo

### /admin/repo/reverseTakedown

POST `?did={did:...}` to reverse a repo take-down

### /admin/repo/compact

POST `?did={did:...}` to compact a repo. Optionally `&fast=true`. HTTP blocks until the compaction finishes.

### /admin/repo/compactAll

POST to begin compaction of all repos. Optional query params:

 * `fast=true`
 * `limit={int}` maximum number of repos to compact (biggest first) (default 50)
 * `threhsold={int}` minimum number of shard files a repo must have on disk to merit compaction (default 20)

### /admin/repo/reset

POST `?did={did:...}` deletes all local data for the repo

### /admin/repo/verify

POST  `?did={did:...}` checks that all repo data is accessible. HTTP blocks until done.

### /admin/pds/requestCrawl

POST `{"hostname":"pds host"}` to start crawling a PDS

### /admin/pds/list

GET returns JSON list of records
```json
[{
  "Host": string,
  "Did": string,
  "SSL": bool,
  "Cursor": int,
  "Registered": bool,
  "Blocked": bool,
  "RateLimit": float,
  "CrawlRateLimit": float,
  "RepoCount": int,
  "RepoLimit": int,
  "HourlyEventLimit": int,
  "DailyEventLimit": int,

  "HasActiveConnection": bool,
  "EventsSeenSinceStartup": int,
  "PerSecondEventRate": {"Max": float, "Window": float seconds},
  "PerHourEventRate": {"Max": float, "Window": float seconds},
  "PerDayEventRate": {"Max": float, "Window": float seconds},
  "CrawlRate": {"Max": float, "Window": float seconds},
  "UserCount": int,
}, ...]
```

### /admin/pds/resync

POST `?host={host}` to start a resync of a PDS

GET `?host={host}` to get status of a PDS resync, return

```json
{"resync": {
  "pds": {
    "Host": string,
    "Did": string,
    "SSL": bool,
    "Cursor": int,
    "Registered": bool,
    "Blocked": bool,
    "RateLimit": float,
    "CrawlRateLimit": float,
    "RepoCount": int,
    "RepoLimit": int,
    "HourlyEventLimit": int,
    "DailyEventLimit": int,
  },
  "numRepoPages": int,
  "numRepos": int,
  "numReposChecked": int,
  "numReposToResync": int,
  "status": string,
  "statusChangedAt": time,
}}
```

### /admin/pds/changeLimits

POST to set the limits for a PDS. body:

```json
{
  "host": string,
  "per_second": int,
  "per_hour": int,
  "per_day": int,
  "crawl_rate": int,
  "repo_limit": int,
}
```

### /admin/pds/block

POST `?host={host}` to block a PDS

### /admin/pds/unblock

POST `?host={host}` to un-block a PDS


### /admin/pds/addTrustedDomain

POST `?domain={}` to make a domain trusted

### /admin/consumers/list

GET returns list json of clients currently reading from the relay firehose

```json
[{
  "id": int,
  "remote_addr": string,
  "user_agent": string,
  "events_consumed": int,
  "connected_at": time,
}, ...]
```
