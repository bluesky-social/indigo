indigo/automod
==============

This package (`github.com/bluesky-social/indigo/automod`) contains a "rules engine" to augment human moderators in the atproto network. Batches of rules are processed for novel "events" such as a new post or update of an account handle. Counters and other statistics are collected, which can drive subsequent rule invocations. The outcome of rules can be moderation events like "report account for human review" or "label post". A lot of what this package does is collect and maintain caches of relevant metadata about accounts and pieces of content, so that rules have efficient access to this information.

A primary design goal is to have a flexible framework to allow new rules to be written and deployed rapidly in response to new patterns of spam and abuse.

Some example rules are included in the `automod/rules` package, but the expectation is that some real-world rules will be kept secret.

Code for subscribing to a firehose is not included here; see `cmd/hepa` for a complete service built on this library.


## Design

Prior art and inspiration:

* The [SQRL language](https://sqrl-lang.github.io/sqrl/) and runtime was originally developed by an industry vendor named Smyte, then acquired by Twitter, with some core Javascript components released open source in 2023. The SQRL documentation is extensive and describes many of the design trade-offs and features specific to rules engines. Bluesky considered adopting SQRL but decided to start with a simpler runtime with rules in a known language (golang).

* Reddit's [automod system](https://www.reddit.com/wiki/automoderator/) is simple an accessible for non-technical sub-reddit community moderators. Discord has a large ecosystem of bots which can help communities manage some moderation tasks, in particular mitigating spam and brigading.

* Facebook's FXL and Haxl rule languages have been in use for over a decade. The 2012 paper ["The Facebook Immune System"](https://css.csail.mit.edu/6.858/2012/readings/facebook-immune.pdf) gives a good overview of design goals and how a rules engine fits in to a an overall anti-spam/anti-abuse pipeline.

* Email anti-spam systems like SpamAssassin and rspamd have been modular and configurable for several decades.
