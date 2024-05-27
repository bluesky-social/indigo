
`bigsky`: atproto Relay Service
===============================

*NOTE: "Relay" used to be called "Big Graph Server", or "BGS", which inspired the name "bigsky". Many variables and packages still reference "bgs"*

This is the implementation of an atproto Relay which is running in the production network, written and operated by Bluesky.

In atproto, a Relay subscribes to multiple PDS hosts and outputs a combined "firehose" event stream. Downstream services can subscribe to this single firehose a get all relevant events for the entire network, or a specific sub-graph of the network. The Relay maintains a mirror of repo data from all accounts on the upstream PDS instances, and verifies repo data structure integrity and identity signatures. It is agnostic to applications, and does not validate data against atproto Lexicon schemas.

This Relay implementation is designed to subscribe to the entire global network. It is informally expected to scale to around 20 million accounts in the network, and thousands of repo events per second (peak).

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


## Development Quickstart

TODO: sqlite commands


## Deployment Quickstart

TODO: reference database section below
TODO: notable env vars
TODO: disclaim that this is a full guide


## Database Setup

PostgreSQL and Sqlite are both supported. When using Sqlite, separate database
for the Relay database itself and the CarStore are used. With PostgreSQL a single
database server, user, and database, can all be reused.

Database configuration is passed via the `DATABASE_URL` and
`CARSTORE_DATABASE_URL` environment variables, or the corresponding CLI args.

For PostgreSQL, the user and database must already be configured. Some example
SQL commands are:

    CREATE DATABASE bgs;
    CREATE DATABASE carstore;

    CREATE USER ${username} WITH PASSWORD '${password}';
    GRANT ALL PRIVILEGES ON DATABASE bgs TO ${username};
    GRANT ALL PRIVILEGES ON DATABASE carstore TO ${username};

This service currently uses `gorm` to automatically run database migrations as
the regular user. There is no concept of running a separate set of migrations
under more privileged database user.

