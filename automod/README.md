`indigo/automod`: rules engine for anti-spam and other moderation tasks
=======================================================================

automod is a "rules engine" framework, used to augment human moderators in the atproto network by proactively identifying patterns of behavior and content. Batches of rules are processed for novel "events" such as a new post or update of an account handle. Counters and other statistics are collected, which can drive subsequent rule invocations. The outcome of rules can be moderation events like "report account for human review" or "label post". Much of what this framework does is simply aggregating and maintaining caches of relevant metadata about accounts and pieces of content, so that rules have efficient access to this information.

A primary design goal is to have a flexible framework to allow new rules to be written and deployed rapidly in response to new patterns of spam and abuse.

Some example rules are included in the `automod/rules` package, but the expectation is that some real-world rules will be kept secret.

Code for subscribing to a firehose is not included here; see `../cmd/hepa` for a service daemon built on this package.

API reference documentation can be found on [pkg.go.dev](https://pkg.go.dev/github.com/bluesky-social/indigo/automod).

## Architecture

The runtime (`automod.Engine`) manages network requests, caching, and configuration. Outside calling code makes concurrent calls to the `Process*` methods that the runtime provides. The runtime constructs event context structs (eg, `automod.RecordContext`), hydrates relevant metadata from (cached) external services, and then executes a configured set of rules on the event. Rules may request additional information (via the `Context` object), do arbitrary local compute, and then update the context with "effects" (such as moderation actions). After all rules have run, the runtime will inspect the context and persist any side-effects, such as updating counter state and pushing any new moderation actions to external services.

The runtime maintains state in several "stores", each of which has an interface and both in-memory and Redis implementations. The automod stores are semi-ephemeral: they are persisted and are important state for rules to work as expected, but they are not a canonical or long-term store for moderation decisions or actions. It is expected that Redis is used in virtually all deployments. The store types are:

- `automod/cachestore`: generic data caching with expiration (TTL) and explicit purging. Used to cache account-level metadata, including identity lookups and (if available) private account metadata
- `automod/countstore`: keyed integer counters with time bucketing (eg, "hour", "day", "total"). Also includes probabilistic "distinct value" counters (eg, Redis HyperLogLog counters, with roughly 2% precision)
- `automod/setstore`: configurable static string sets. May eventually be runtime configurable
- `automod/flagstore`: mechanism to keep track of automod-generated "flags" (like labels or hashtags) on accounts or records. Mostly used to detect *new* flags. May eventually be moved in to the moderation service itself, similar to labels

## Prior Art

* The [SQRL language](https://sqrl-lang.github.io/sqrl/) and runtime was originally developed by an industry vendor named Smyte, then acquired by Twitter, with some core Javascript components released open source in 2023. The SQRL documentation is extensive and describes many of the design trade-offs and features specific to rules engines. Bluesky considered adopting SQRL but decided to start with a simpler runtime with rules in a known language (golang).

* Reddit's [automod system](https://www.reddit.com/wiki/automoderator/) is simple an accessible for non-technical sub-reddit community moderators. Discord has a large ecosystem of bots which can help communities manage some moderation tasks, in particular mitigating spam and brigading.

* Facebook's FXL and Haxl rule languages have been in use for over a decade. The 2012 paper ["The Facebook Immune System"](https://css.csail.mit.edu/6.858/2012/readings/facebook-immune.pdf) gives a good overview of design goals and how a rules engine fits in to a an overall anti-spam/anti-abuse pipeline.

* Email anti-spam systems like SpamAssassin and rspamd have been modular and configurable for several decades.
