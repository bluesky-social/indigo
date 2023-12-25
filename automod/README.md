`indigo/automod`
================

This package (`github.com/bluesky-social/indigo/automod`) contains a "rules engine" to augment human moderators in the atproto network. Batches of rules are processed for novel "events" such as a new post or update of an account handle. Counters and other statistics are collected, which can drive subsequent rule invocations. The outcome of rules can be moderation events like "report account for human review" or "label post". A lot of what this package does is collect and maintain caches of relevant metadata about accounts and pieces of content, so that rules have efficient access to this information.

A primary design goal is to have a flexible framework to allow new rules to be written and deployed rapidly in response to new patterns of spam and abuse.

Some example rules are included in the `automod/rules` package, but the expectation is that some real-world rules will be kept secret.

Code for subscribing to a firehose is not included here; see `../cmd/hepa` for a service daemon built on this package.

API reference documentation can be found on [pkg.go.dev](https://pkg.go.dev/github.com/bluesky-social/indigo/automod).

## Architecture

The runtime (`automod.Engine`) manages network requests, caching, and configuration. Outside calling code makes concurrent calls to the `Process*` methods that the runtime provides. The runtime constructs event context structs (eg, `automod.RecordContext`), hydrates relevant metadata from (cached) external services, and then executes a configured set of rules on the event. Rules may request additional context, do arbitrary local compute, and update the context with "effects" (such as moderation actions). After all rules have run, the runtime will inspect the context and persist any side-effects, such as updating counter state and pushing any new moderation actions to external services.

The runtime keeps state in several "stores", each of which has an interface and both in-memory and Redis implementations. It is expected that Redis is used in virtually all deployments. The store types are:

- `automod/cachestore`: generic data caching with expiration (TTL) and explicit purging. Used to cache account-level metadata, including identity lookups and (if available) private account metadata
- `automod/countstore`: keyed integer counters with time bucketing (eg, "hour", "day", "total"). Also includes probabilistic "distinct value" counters (eg, Redis HyperLogLog counters, with roughly 2% precision)
- `automod/setstore`: configurable static string sets. May eventually be runtime configurable
- `automod/flagstore`: mechanism to keep track of automod-generated "flags" (like labels or hashtags) on accounts or records. Mostly used to detect *new* flags. May eventually be moved in to the moderation service itself, similar to labels


## Rule API

Here is a simple example rule, which handles creation of new events:

```golang
var gtubeString = "XJS*C4JDBQADN1.NSBN3*2IDNEN*GTUBE-STANDARD-ANTI-UBE-TEST-EMAIL*C.34X"

func GtubePostRule(c *automod.RecordContext, post *appbsky.FeedPost) error {
	if strings.Contains(post.Text, gtubeString) {
		c.AddRecordLabel("spam")
	}
	return nil
}
```

Every new post record will be inspected to see if it contains a static test string. If it does, the label `spam` will be applied to the record itself.

The `c` parameter provides access to relevant pre-fetched metadata; methods to fetch additional metadata from the network; a `slog` logging interface; and methods to store output decisions. The runtime will catch and recover from unexpected panics, and will log returned errors, but rules are generally expected to run robustly and efficiently, and not have complex control flow needs.

Some of the more commonly used features of `c` (`automod.RecordContext`):

- `c.Logger`: a `log/slog` logging interface
- `c.Account.Identity`: atproto identity for the author account, including DID, handle, and PDS endpoint
- `c.Account.Private`: when not-null (aka, when the runtime has administrator access) will contain things like `.IndexedAt` (account first seen) and `.Email` (the current registered account email)
- `c.Account.Profile`: a cached subset of the account's `app.bsky.actor.profile` record (if non-null)
- `c.GetCount(<namespace>, <value>, <time-period>)` and `c.Increment(<namespace>, <value>)`: to access and update simple counters (by hour, day, or total). Incrementing counters is lazy and happens in batch after all rules have executed: this means that multiple calls are de-duplicated, and that `GetCount` will not reflect any prior `Increment` calls in the same rule (or between rules).
- `c.GetCountDistinct(<namespace>, <bucket>, <time-period>)` and `c.IncrementDistinct(<namespace>, <bucket>, <value>)`: similar to simple counters, but counts "unique distinct values"
- `c.InSet(<set-name>, <value>)`: checks if a string is in a named set

Notice that few (or none) of the context methods return errors. Errors are accumulated internally on the context itself, and error handling takes place before any effects are persisted by the engine.


## Developing New Rules

The current tl;dr process to deploy a new rule:

- copy a similar existing rule from `automod/rules`
- add the new rule to a `RuleSet`, so it will be invoked
- test against content that triggers the new rule
- deploy

You'll usually want to start with both a known pattern you are looking for, and some example real-world content which you want to trigger on.

The `automod/rules` package contains a set of example rules and some shared helper functions, and demonstrates some patterns for how to use counters, sets, filters, and account metadata to compose a rule pattern.

The `hepa` command provides `process-record` and `process-recent` sub-commands which will pull an existing individual record (by AT-URI) or all recent bsky posts for an account (by handle or DID), which can be helpful for testing.

When deploying a new rule, it is recommended to start with a minimal action, like setting a flag or just logging. Any "action" (including new flag creation) can result in a Slack notification. You can gain confidence in the rule by running against the full firehose with these limited actions, tweaking the rule until it seems to have acceptable sensitivity (eg, few false positives), and then escalate the actions to reporting (adds to the human review queue), or action-and-report (label or takedown, and concurrently report for humans to review the action).


## Prior Art

* The [SQRL language](https://sqrl-lang.github.io/sqrl/) and runtime was originally developed by an industry vendor named Smyte, then acquired by Twitter, with some core Javascript components released open source in 2023. The SQRL documentation is extensive and describes many of the design trade-offs and features specific to rules engines. Bluesky considered adopting SQRL but decided to start with a simpler runtime with rules in a known language (golang).

* Reddit's [automod system](https://www.reddit.com/wiki/automoderator/) is simple an accessible for non-technical sub-reddit community moderators. Discord has a large ecosystem of bots which can help communities manage some moderation tasks, in particular mitigating spam and brigading.

* Facebook's FXL and Haxl rule languages have been in use for over a decade. The 2012 paper ["The Facebook Immune System"](https://css.csail.mit.edu/6.858/2012/readings/facebook-immune.pdf) gives a good overview of design goals and how a rules engine fits in to a an overall anti-spam/anti-abuse pipeline.

* Email anti-spam systems like SpamAssassin and rspamd have been modular and configurable for several decades.
