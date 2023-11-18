// Auto-Moderation rules engine for anti-spam and other moderation tasks.
//
// This package (`github.com/bluesky-social/indigo/automod`) contains a "rules engine" to augment human moderators in the atproto network. Batches of rules are processed for novel "events" such as a new post or update of an account handle. Counters and other statistics are collected, which can drive subsequent rule invocations. The outcome of rules can be moderation events like "report account for human review" or "label post". A lot of what this package does is collect and maintain caches of relevant metadata about accounts and pieces of content, so that rules have efficient access to this information.
//
// A primary design goal is to have a flexible framework to allow new rules to be written and deployed rapidly in response to new patterns of spam and abuse. Some examples rules are included in the `automod/rules` package, but the expectation is that some real-world rules will be kept secret.
//
// Code for subscribing to a firehose is not included here; see cmd/hepa for a complete service built on this library.
// The `hepa` command is an example daemon which integrates this rules engine in to a stand-alone service.
package automod
