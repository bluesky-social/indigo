
hepa (indigo edition)
=====================

This is a simple auto-moderation daemon which wraps the automod package. This is public code. The actual version run by Bluesky is similar, but a private fork to protect methods and mechanisms.

The name is a reference to HEPA air filters, which help keep the local atmosphere clean and healthy for humans.

Available commands, flags, and config are documented in the usage (`--help`).

Current features and design decisions:

- all state (counters) and caches stored in Redis
- consumes from Relay firehose; no backfill functionality yet
- which rules are included configured at compile time
- admin access to fetch private account metadata, and to persist moderation actions, is optional. it is possible for anybody to run a `hepa` instance

This is not a "labeling service" per say, in that it pushes labels in to an existing moderation service, and doesn't provide API endpoints or label streams.

Performance is generally slow when first starting up, because account-level metadata is being fetched (and cached) for every firehose event. After the caches have "warmed up", events are processed faster.

See the `automod` package's README for more documentation.
