
bluepages: an atproto identity directory
========================================

This is a simple API server which caches atproto handle and DID resolution responses. It is useful when you have a bunch of services that do identity resolution, and you don't want duplicated caches.

Available commands, flags, and config are documented in the usage (`--help`).

Current features and design decisions:

- all caches stored in Redis
- will consume from the firehose (but doesn't yet)
- Lexicon API endpoints:
  - `GET com.atproto.identity.resolveHandle`
  - `GET com.atproto.identity.resolveDid`
  - `GET com.atproto.identity.resolveIdentity`
  - `POST com.atproto.identity.refreshIdentity` (admin auth)
