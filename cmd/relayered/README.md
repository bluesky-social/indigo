
relayered: an atproto relay
===========================

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

    RELAY_ADMIN_KEY=localdev go run ./cmd/relayered/ --help

By default, the daemon will use sqlite for databases (in the directory `./data/relayered/`) and the HTTP API will be bound to localhost port 2470.

When the daemon isn't running, sqlite database files can be inspected with:

    sqlite3 data/relayered/relayered.sqlite
    [...]
    sqlite> .schema

Wipe all local data:

    # careful! double-check this destructive command
    rm -rf ./data/relayered/*

There is a basic web dashboard, though it will not be included unless built and copied to a local directory `./public/`. Run `make build-relay-ui`, and then when running the daemon the dashboard will be available at: <http://localhost:2470/dash/>. Paste in the admin key, eg `localdev`.

The local admin routes can also be accessed by passing the admin key as a bearer token, for example:

    http get :2470/admin/pds/list Authorization:"Bearer localdev"

Request crawl of an individual PDS instance like:

    http post :2470/admin/pds/requestCrawl Authorization:"Bearer localdev" hostname=pds.example.com


## Docker Containers

One way to deploy is running a docker image. You can pull and/or run a specific version of relay, referenced by git commit, from the Bluesky Github container registry. For example:

    docker pull ghcr.io/bluesky-social/indigo:relayered-fd66f93ce1412a3678a1dd3e6d53320b725978a6
    docker run ghcr.io/bluesky-social/indigo:relayered-fd66f93ce1412a3678a1dd3e6d53320b725978a6

There is a Dockerfile in this directory, which can be used to build customized/patched versions of the relay as a container, republish them, run locally, deploy to servers, deploy to an orchestrated cluster, etc. See docs and guides for docker and cluster management systems for details.


## Database Setup

PostgreSQL and Sqlite are both supported. Database configuration is passed via the `DATABASE_URL` environment variable, or the corresponding CLI arg.

For PostgreSQL, the user and database must already be configured. Some example SQL commands are:

    CREATE DATABASE relayered;

    CREATE USER ${username} WITH PASSWORD '${password}';
    GRANT ALL PRIVILEGES ON DATABASE relayered TO ${username};

This service currently uses `gorm` to automatically run database migrations as the regular user. There is no concept of running a separate set of migrations under more privileged database user.


## Deployment

*NOTE: this is not a complete guide to operating a relay. There are decisions to be made and communicated about policies, bandwidth use, PDS crawling and rate-limits, financial sustainability, etc, which are not covered here. This is just a quick overview of how to technically get a relay up and running.*

In a real-world system, you will probably want to use PostgreSQL.

Some notable configuration env vars to set:

- `ENVIRONMENT`: eg, `production`
- `DATABASE_URL`: see section below
- `GOLOG_LOG_LEVEL`: log verbosity

There is a health check endpoint at `/xrpc/_health`. Prometheus metrics are exposed by default on port 2471, path `/metrics`. The service logs fairly verbosely to stderr; use `GOLOG_LOG_LEVEL` to control log volume.

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
relayered  --api-listen :2470 --admin-key ${RELAY_ADMIN_PASSWORD} ...
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
