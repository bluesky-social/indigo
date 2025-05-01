

## Behaviors

Details about how the relay operates which might not be obvious!

- unknown/unexpected fields on overall firehose messages (eg, `#commit`) are *not* passed-through, so it is critical to upgrade the relay when there are protocol changes
- records and commit objects *are* passed through verbatim: they are serialized in `blocks` fields on `#commit` and `#sync` messages
- some admin UI changes are persisted across restarts (stored in database), others are not (ephemeral)
  - ephemeral (but can be configured via env vars): new-hosts-per-day limit; enable/disable requestCrawl
  - persisted (in database): account takedowns, domain bans, host bans, host account limit
- the "lenient mode" configuration flag is intended as a short-term migration tool for [atproto Sync 1.1](https://github.com/bluesky-social/proposals/tree/main/0006-sync-iteration) and will be removed over time
- once an upstream host websocket is established, the sequence numbers on that socket must always increase; messages with lower sequence will be dropped. but this is only strictly enforced over the life the the socket connection; if the relay restarts and the host emits older sequence numbers, those messages will start coming through
- for a new host (no known previous sequence number), the relay will connect at "current" firehose offset, not "oldest" offset and backfill
- for a known host, the relay will attempt to reconnect (eg, after a drop or restart) at the last persisted sequence number. persisting should happen every few seconds, or at clean shutdown of the daemon, but it is possible for the cursor to be slightly out of sync, resulting in replay of messages
- account-level `#commit` revisions must always increase, and these revisions are stored for every valid `#commit` or `#sync` message from the account. repeated or lower revision messages are dropped. messages with revisions corresponding to a TID "in the future" (beyond a fudge period of a few minutes) are also dropped
- messages for an account (DID) which come from a host connection which are not the current PDS host for that account are dropped. If there is a mismatch, the relay will re-resolve the identity (DID document) and double-check before dropping the message, in case there was an account migration not reflected yet in local caches.
- if a host sends no messages for a long period, the relay will drop the connection and set the host status to "idle"; this is common for low-traffic PDS instances (eg, handful of accounts). The expectation is that the host would then send a `requestCrawl` ping next time there is a new event.
- when the relay restarts, it connects to all "active" hosts
- if configured with "sibling" relay instances, will forward `requestCrawl` and some administrative requests to each of those instances. The use-case is to keep a cluster of independent relays relatively synchronized in terms of hosts subscribed, takedowns, and quotas. Requests are only forwarded if processed successfully on the current instance. `User-Agent` is passed through from original request, but the `Via` header is set, and used to prevent forwarding loops. Auth headers are passed through; admin forwarding only works if the same secret works for all sibling relays. API requests forwarded to a remote rainbow instance (in front of a relay), should get proxied through to that relay successfully.
- both the relay and rainbow set a `Server` header in HTTP responses (including WebSocket connections), and the relay checks for this header when connecting. If it finds the string `atproto-relay` in the header, it refuses the connection, to prevent relay request loops. This is just a conservative default behavior; relays consuming from other relays is allowed by protocol.
- when connecting to remote hosts, including WebSocket subscriptions, the relay includes basic SSRF protections against connecting to private, reserved, or local IP addresses; or ports other than 80 or 443. This check is skipped if the remote host is specifically localhost (with an explicit port). If needed this constraint could be made configurable.


## Internal Implementation Details

- the parallel event scheduler prevents multiple tasks for the same account (DID) from being processed at the same time
- note the potential for race-conditions with messages about the same account (DID) coming from different hosts around the same time: in this case there is no guarantee about ordering
- the relay keeps track of which events have been received-but-not-processed by sequence number, and only increments the `lastSeq` for actually-processed events. the "inflight" set of messages (sequence numbers) can grow rather large for active hosts, if there are many events for a single account (only one processed per account at a time)


## Code Organization and History

*Note: this was written in April 2025, and is likely to get out of date*

This codebase started as a fork of the prior `bigsky` / "BGS" relay implementation. The host and account state management, and message validation, were re-written. The "slurper" got a refactor, and some event stream and disk persistence code got lighter changes.

- `Service` struct: overall service executable/daemon. Implements protocol and admin HTTP endpoints.
- `relay.Relay` struct: core relay service logic, message validation and processing, state and database management
- `relay.Slurper` struct: maintains active subscriptions (WebSocket connections) to upstream hosts (eg, PDS instances)
- `relay/models` package: database models
- `stream` package: fork of `indigo:events` package, including websocket "frame" type, listeners, and some event stream rate-limiting
- `stream.XRPCStreamEvent` struct: relatively critical/central serialiation type
- `stream.eventmgr.EventManager`: manages output firehose: disk persistence, sequencing, etc
- `testing` package: end-to-end integration tests

The `stream` code should probably get merged back in with the `indigo:events` at some point, but there are many small differences so it won't be a quick/trivial change.


## Verification Tools and Tests

- `goat` has several firehose verify flags
- `./testing/` contains a framework for end-to-end relay integration tests
- commit-level MST slice validation tests are in `indigo:atproto/repo`
- there are some interop test resources at: https://github.com/bluesky-social/atproto-interop-tests
