
# CHANGELOG

All notable changes to this project will be documented in this file. The format
is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).


## [Unreleased]

- Add draft permissioned Spaces syntax, authority resolution, OAuth scope and Lexicon permission/schema support.
- Add immutable `atproto/spaces` read handles, explicit credential exchange, HTTP Message Signatures, notification authentication, paged reads, and caller-owned CAR/blob streams with named protocol errors.
- Add `atproto/atcrypto/lthash`, protocol commit JSON/CBOR codecs, corrected commit HMAC, and offline record/blob/ordered CAR verification with explicit index-only mode.
- Add scoped Spaces RPCs and opt-in modern `cmd/lexgen` generation with independent package targeting, raw unknown values, and lossless unknown open unions. Established APIs are not regenerated.


## [d0ea072] - 2023-03-31

Large Lexicon refactor, and updates to streaming event wire schemas. The
relevant typescript repo pull request, with more details, is
<https://github.com/bluesky-social/atproto/pull/658>.


## [61fc4c0] - 2023-03-10

MST and repo metadata schema refactor, resulting in repo binary format v2.


## [init] - 2023-01-19

Forked this repo from [whyrusleeping/gosky](https://github.com/whyrusleeping/gosky).
