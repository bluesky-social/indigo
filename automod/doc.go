// Auto-Moderation rules engine for anti-spam and other moderation tasks.
//
// This package (`github.com/gander-social/gander-indigo-sovereign/automod`) contains a "rules engine" to augment human moderators in the atproto network. Batches of rules are processed for novel "events" such as a new post or update of an account handle. Counters and other statistics are collected, which can drive subsequent rule invocations. The outcome of rules can be moderation events like "report account for human review" or "label post". A lot of what this package does is collect and maintain caches of relevant metadata about accounts and pieces of content, so that rules have efficient access to this information.
//
// See `automod/README.md` for more background, and `cmd/hepa` for a daemon built on this package.
package automod
