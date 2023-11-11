// Auto-Moderation rules engine for anti-spam and other moderation tasks.
//
// The code in this package includes an "engine" which processes atproto commit events (and identity updates), maintains caches and counters, and pushes moderation decisions to an external mod service (eg, appview). A framework for writing new "rules" for the engine to execute are also provided.
//
// It does not provide label API endpoints like queryLabels; see labelmaker for a self-contained labeling service.
//
// Code for subscribing to a firehose is not included here; see cmd/hepa for a complete service built on this library.
package automod
