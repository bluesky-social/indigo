![photo](https://static.bnewbold.net/tmp/indigo_serac.jpeg)

# indigo: atproto libraries and services in golang

Some Bluesky software is developed in Typescript, and lives in the [bluesky-social/atproto](https://github.com/bluesky-social/atproto) repository. Some is developed in Go, and lives here.

**If you are not a Go developer and you want to run one of these tools**, you can do:

```bash
# with [Homebrew](brew.sh) installed
brew install go
# for example, to run tap
go install github.com/bluesky-social/indigo/cmd/tap
tap
```

Go will fetch dependencies, compile, and install `tap` or another service with a one-line `go install` command.

*Soon*, we plan to decouple the tools in this repo so you can install them individually like [goat](https://formulae.brew.sh/formula/goat).

## What is in here?

**Go Services:**

- **tap** ([README](./cmd/tap/README.md)): synchronization and backfill tool for atproto apps
- **relay** ([README](./cmd/relay/README.md)): relay reference implementation
- **rainbow** ([README](./cmd/rainbow/README.md)): firehose "splitter" or "fan-out" service
- **hepa** ([README](./cmd/hepa/README.md)): auto-moderation bot for [Ozone](https://ozone.tools)

**Developer Tools:**

- **goat** ([README](https://github.com/bluesky-social/goat)): CLI for interacting with network: CAR files, firehose, APIs, etc (moved to [separate repo](https://github.com/bluesky-social/goat))

**Go Packages:**

> ⚠️ All the packages in this repository are under active development. Features and software interfaces have not stabilized and may break or be removed.

| Package                                                      | Docs                                                                                                                                                                    |
| ------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `api/atproto`: generated types for `com.atproto.*` Lexicons  | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/bluesky-social/indigo/api/atproto)](https://pkg.go.dev/mod/github.com/bluesky-social/indigo/api/atproto)           |
| `api/bsky`: generated types for `app.bsky.*` Lexicons        | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/bluesky-social/indigo/api/bsky)](https://pkg.go.dev/mod/github.com/bluesky-social/indigo/api/bsky)                 |
| `atproto/atclient`: HTTP API client                          | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/bluesky-social/indigo/atproto/atclient)](https://pkg.go.dev/mod/github.com/bluesky-social/indigo/atproto/atclient) |
| `atproto/auth/oauth`: AT OAuth client                        | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/bluesky-social/indigo/atproto/auth/oauth)](https://pkg.go.dev/mod/github.com/bluesky-social/indigo/atproto/auth/oauth)|
| `atproto/identity`: DID and handle resolution                | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/bluesky-social/indigo/atproto/identity)](https://pkg.go.dev/mod/github.com/bluesky-social/indigo/atproto/identity) |
| `atproto/spaces`: draft permissioned Spaces client and verification | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/bluesky-social/indigo/atproto/spaces)](https://pkg.go.dev/mod/github.com/bluesky-social/indigo/atproto/spaces) |
| `atproto/syntax`: string types and parsers for identifiers   | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/bluesky-social/indigo/atproto/syntax)](https://pkg.go.dev/mod/github.com/bluesky-social/indigo/atproto/syntax)     |
| `atproto/lexicon`: schema validation of data                 | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/bluesky-social/indigo/atproto/lexicon)](https://pkg.go.dev/mod/github.com/bluesky-social/indigo/atproto/lexicon)   |
| `atproto/repo`: repository data structure                    | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/bluesky-social/indigo/atproto/repo)](https://pkg.go.dev/mod/github.com/bluesky-social/indigo/atproto/repo)         |
| `atproto/repo/mst`: Merkle Search Tree implementation        | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/bluesky-social/indigo/atproto/repo/mst)](https://pkg.go.dev/mod/github.com/bluesky-social/indigo/atproto/repo/mst) |
| `atproto/atcrypto`: cryptographic signing and key serialization | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/bluesky-social/indigo/atproto/atcrypto)](https://pkg.go.dev/mod/github.com/bluesky-social/indigo/atproto/atcrypto)|
| `atproto/atcrypto/lthash`: incremental set hash | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/bluesky-social/indigo/atproto/atcrypto/lthash)](https://pkg.go.dev/mod/github.com/bluesky-social/indigo/atproto/atcrypto/lthash) |
| `go-didplc/didplc`: DID PLC implementation (external) | [![PkgGoDev](https://pkg.go.dev/badge/mod/github.com/did-method-plc/go-didplc/didplc)](https://pkg.go.dev/mod/github.com/did-method-plc/go-didplc/didplc)|

The TypeScript reference implementation, including PDS and bsky AppView services, is at [bluesky-social/atproto](https://github.com/bluesky-social/atproto). Source code for the Bluesky Social client app (for web and mobile) can be found at [bluesky-social/social-app](https://github.com/bluesky-social/social-app).

## Permissioned Spaces (draft)

`atproto/spaces` implements the [Spaces proposal](https://github.com/bluesky-social/proposals/tree/main/0016-permissioned-data): immutable read handles, explicit credential exchange, notifications, and offline verification. The protocol is a draft. Public repository APIs and `syntax.ATURI` semantics are unchanged; Spaces records use `syntax.SpaceURI` and spaces use `syntax.SpaceRef`.

An already-issued token and its bound P-256 signer can be used without an OAuth session or another exchange:

```go
client := spaces.NewClient(spaces.ClientOptions{Directory: directory})
credential, err := spaces.NewCredential(issuedToken, boundSigner)
if err != nil {
    return err
}
repo := client.Space(ref, credential).Repo(author)
stream, err := repo.GetRepo(ctx, spaces.GetRepoOptions{})
if err != nil {
    return err
}
defer stream.Close()
verified, err := spaces.VerifyRepo(ctx, stream, spaces.RepoVerificationOptions{
    Space: ref, Author: author, PublicKey: authorKey,
}, consume)
```

Constructors do not resolve identities or issue credentials. Space-host operations resolve the authority and sign for its DID; repo operations resolve the author's PDS and sign for that author's DID, including authors sharing a PDS. To obtain a credential, call `GetDelegationToken` with the existing authenticated PDS client and then `ExchangeCredential` with a fresh signer. Replacement credentials require new handles; existing handles keep their original token/key pair.

Raw reads are not verification claims. Callers close successful CAR/blob streams. `VerifyRepo` validates the commit, canonical index, LtHash digest, and ordered record blocks through EOF; consumers must stage callback output until it succeeds and copy borrowed record bytes they retain. `IndexOnly: true` explicitly verifies a commit/index-only CAR without claiming record verification. Blob verification is separate. Discovery pages report the authority's view, can repeat repos, and do not form an atomic inventory.

Notification registration takes a service DID with an optional fragment, not a callback URL. `VerifyNotificationAuth` binds the issuer, audience, and endpoint. Own-repo writes use the generated `api/atproto` RPCs with the existing OAuth-authenticated PDS client, not read credentials. Simplespace management is excluded from the current syncing-tool review. Consumers own scheduling, persistence, checkpoints, concurrency, credential replacement, and recovery; the library does not implement a sync loop or automatic renewal/deletion.

Local HTTP and independently constructed signature, HMAC, and CAR scenarios have been exercised. No live Spaces service has been exercised.

## Development Quickstart

First, you will need the Go toolchain installed. We develop using the latest stable version of the language.

The Makefile provides wrapper commands for basic development:

    make build
    make test
    make fmt
    make lint

Individual commands can be run like:

    go run ./cmd/relay

The [HACKING](./HACKING.md) file has a list of commands and packages in this repository and some other development tips.

## What is atproto?

_not to be confused with the [AT command set](https://en.wikipedia.org/wiki/Hayes_command_set) or [Adenosine triphosphate](https://en.wikipedia.org/wiki/Adenosine_triphosphate)_

The Authenticated Transfer Protocol ("ATP" or "atproto") is a decentralized social media protocol, developed by [Bluesky Social PBC](https://bsky.social). Learn more at:

- [Overview and Guides](https://atproto.com/guides/overview) 👈 Best starting point
- [Github Discussions](https://github.com/bluesky-social/atproto/discussions) 👈 Great place to ask questions
- [Protocol Specifications](https://atproto.com/specs/atp)
- [Blogpost on self-authenticating data structures](https://bsky.social/about/blog/3-6-2022-a-self-authenticating-social-protocol)

The Bluesky Social application encompasses a set of schemas and APIs built in the overall AT Protocol framework. The namespace for these "Lexicons" is `app.bsky.*`.

## Contributions

> While we do accept contributions, we prioritize high quality issues and pull requests. Adhering to the below guidelines will ensure a more timely review.

**Rules:**

- We may not respond to your issue or PR.
- We may close an issue or PR without much feedback.
- We may lock discussions or contributions if our attention is getting DDOSed.
- We do not provide support for build issues.

**Guidelines:**

- Check for existing issues before filing a new one, please.
- Open an issue and give some time for discussion before submitting a PR.
- Issues are for bugs & feature requests related to the golang implementation of atproto and related services.
  - For high-level discussions, please use the [Discussion Forum](https://github.com/bluesky-social/atproto/discussions).
  - For client issues, please use the relevant [social-app](https://github.com/bluesky-social/social-app) repo.
- Stay away from PRs that:
  - Refactor large parts of the codebase
  - Add entirely new features without prior discussion
  - Change the tooling or libraries used without prior discussion
  - Introduce new unnecessary dependencies

Remember, we serve a wide community of users. Our day-to-day involves us constantly asking "which top priority is our top priority." If you submit well-written PRs that solve problems concisely, that's an awesome contribution. Otherwise, as much as we'd love to accept your ideas and contributions, we really don't have the bandwidth.

## Are you a developer interested in building on atproto?

Bluesky is an open social network built on the AT Protocol, a flexible technology that will never lock developers out of the ecosystems that they help build. With atproto, third-party can be as seamless as first-party through custom feeds, federated services, clients, and more.

## License

This project is dual-licensed under MIT and Apache 2.0 terms:

- MIT license ([LICENSE-MIT](https://github.com/bluesky-social/indigo/blob/main/LICENSE-MIT) or http://opensource.org/licenses/MIT)
- Apache License, Version 2.0, ([LICENSE-APACHE](https://github.com/bluesky-social/indigo/blob/main/LICENSE-APACHE) or http://www.apache.org/licenses/LICENSE-2.0)

Downstream projects and end users may chose either license individually, or both together, at their discretion. The motivation for this dual-licensing is the additional software patent assurance provided by Apache 2.0.

Bluesky Social PBC has committed to a software patent non-aggression pledge. For details see [the original announcement](https://bsky.social/about/blog/10-01-2025-patent-pledge).
